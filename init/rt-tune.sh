#!/bin/sh
#
# Move hardware interrupts off the isolated CPU so the haptic audio producer
# owns that core. Interrupt affinity resets at every boot, which is why this
# runs from simtezilo.service as an ExecStartPre rather than once by hand.
#
# This script deliberately does only the per-boot work. The static half of the
# tuning — the isolcpus kernel command line, the CPUAffinity drop-in, and the
# irqbalance ban list — is applied once at provisioning time, either baked into
# the generated image or by hand on an existing device. See
# doc/realtime_tuning.md for those steps.
#
# It is safe to run on an untuned machine: with no isolated CPU it prints a
# message and exits 0. simtezilo.service invokes it with a "-" prefix, so even
# a hard failure cannot stop the application from starting.
#
# See doc/realtime_tuning.md for the reasoning and the measurement procedure.

set -e

if [ "$(id -u)" -ne 0 ]; then
    echo "This script must be run as root"
    exit 1
fi

isolatedPath='/sys/devices/system/cpu/isolated'

# /run is a tmpfs cleared at boot, so this marker makes the script idempotent
# within a boot without making it skip the work after a reboot. It matters
# because simtezilo.service has Restart=always: without the marker a crash loop
# would rewrite every interrupt mask every five seconds.
markerPath='/run/simtezilo-rt-tuned'

if [ -e "${markerPath}" ]; then
    echo "Interrupt affinity already applied this boot, nothing to do"
    exit 0
fi

if [ ! -r "${isolatedPath}" ]; then
    echo "Core isolation not configured (no ${isolatedPath}): nothing to do."
    exit 0
fi

isolated=$(cat "${isolatedPath}")

if [ -z "${isolated}" ]; then
    # This is the normal state, not a fault. Realtime priority (the part that
    # matters) is set by the application itself and needs nothing from here.
    # Core isolation is a separate, optional step; see doc/realtime_tuning.md.
    echo "Core isolation not configured: nothing to do."
    exit 0
fi

echo "Isolated CPUs: ${isolated}"

# expandCPUList prints every CPU in a kernel CPU list, one per line. The kernel
# writes ranges such as "0-2,5", so a plain string match is not enough: matching
# ",2," against ",2-3," fails and would treat an isolated CPU as shared.
expandCPUList() {
    for field in $(echo "$1" | tr ',' ' '); do
        case "${field}" in
            *-*)
                low=${field%-*}
                high=${field#*-}

                while [ "${low}" -le "${high}" ]; do
                    echo "${low}"
                    low=$((low + 1))
                done
                ;;
            *) echo "${field}" ;;
        esac
    done
}

# isCPUInList reports whether cpu appears in a kernel CPU list.
isCPUInList() {
    for entry in $(expandCPUList "$2"); do
        if [ "${entry}" = "$1" ]; then
            return 0
        fi
    done

    return 1
}

# Build the complement: every CPU that interrupts may still use.
cpuCount=$(nproc)
shared=''
cpu=0

while [ "${cpu}" -lt "${cpuCount}" ]; do
    if ! isCPUInList "${cpu}" "${isolated}"; then
        shared="${shared}${shared:+,}${cpu}"
    fi

    cpu=$((cpu + 1))
done

if [ -z "${shared}" ]; then
    echo "Every CPU is isolated. Refusing to move interrupts with nowhere to send them."
    exit 1
fi

echo "Shared CPUs for interrupts: ${shared}"

# Move every interrupt that will accept a new mask off the isolated CPUs. Some
# interrupts are pinned by hardware and reject the write, which is expected, so
# the failures are counted rather than treated as errors.
moveInterrupts() {
    moved=0
    refused=0

    for irqPath in /proc/irq/*/smp_affinity_list; do
        [ -w "${irqPath}" ] || continue

        if echo "${shared}" > "${irqPath}" 2>/dev/null; then
            moved=$((moved + 1))
        else
            refused=$((refused + 1))
        fi
    done

    echo "Interrupt affinity: ${moved} moved, ${refused} refused (hardware pinned)"
}

# showServiceAffinity reports the CPUs this service may run on, and warns when a
# core is isolated but not excluded. That half-provisioned state is the one that
# looks tuned and is not: the core is reserved, and every thread still uses it.
#
# systemd applies CPUAffinity to every process it starts for the unit, this
# script included, so the script's own allowed list is an accurate test.
showServiceAffinity() {
    allowed=$(awk '/^Cpus_allowed_list:/ { print $2 }' /proc/self/status 2>/dev/null || true)

    if [ -z "${allowed}" ]; then
        return 0
    fi

    echo "Service CPU affinity: ${allowed}"

    for iso in $(expandCPUList "${isolated}"); do
        if isCPUInList "${iso}" "${allowed}"; then
            echo "WARNING: CPU ${iso} is isolated, but this service may still run on it."
            echo "         The reserved core will be used by every thread anyway."
            echo "         Set CPUAffinity to exclude it. See doc/realtime_tuning.md."

            return 0
        fi
    done
}

# Report what is left so the effect is verifiable without a second command.
showRemaining() {
    remaining=0

    for irqPath in /proc/irq/*/smp_affinity_list; do
        [ -r "${irqPath}" ] || continue

        mask=$(cat "${irqPath}" 2>/dev/null) || continue
        irq=$(echo "${irqPath}" | cut -d/ -f4)

        # The mask is itself a range such as "0-3", so compare CPU by CPU. Stop
        # at the first match, or an irq allowed on two isolated CPUs is counted
        # twice and printed twice.
        for iso in $(expandCPUList "${isolated}"); do
            if isCPUInList "${iso}" "${mask}"; then
                echo "  irq ${irq}: ${mask}"
                remaining=$((remaining + 1))

                break
            fi
        done
    done

    if [ "${remaining}" -eq 0 ]; then
        echo '  none'
    fi
}

moveInterrupts

echo ''
echo 'Interrupts still allowed on an isolated CPU:'
showRemaining

echo ''
showServiceAffinity

touch "${markerPath}"
