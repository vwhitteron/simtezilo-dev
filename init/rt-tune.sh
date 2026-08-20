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

# Build the complement: every CPU that interrupts may still use.
cpuCount=$(nproc)
shared=''
cpu=0

while [ "${cpu}" -lt "${cpuCount}" ]; do
    case ",${isolated}," in
        *",${cpu},"*) ;;
        *) shared="${shared}${shared:+,}${cpu}" ;;
    esac

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

# Report what is left so the effect is verifiable without a second command.
showRemaining() {
    remaining=0

    for irqPath in /proc/irq/*/smp_affinity_list; do
        [ -r "${irqPath}" ] || continue

        mask=$(cat "${irqPath}" 2>/dev/null) || continue
        irq=$(echo "${irqPath}" | cut -d/ -f4)

        for iso in $(echo "${isolated}" | tr ',' ' '); do
            case ",${mask}," in
                *",${iso},"*)
                    echo "  irq ${irq}: ${mask}"
                    remaining=$((remaining + 1))
                    ;;
            esac
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

touch "${markerPath}"
