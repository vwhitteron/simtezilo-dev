#!/bin/sh

if [ $(whoami) != 'root' ]; then
    echo "This script must be run as root"
    exit 1
fi

if [ -z "$1" ]; then
    echo "Usage: $0 <hw> [realtime]"
    echo ""
    echo "  hw        one of: none, pirateaudio, waveshare, realtime"
    echo "  realtime  also reserve a CPU core for the haptic audio producer."
    echo "            Opt-in: measure before enabling it. See doc/realtime_tuning.md."
    echo ""
    echo "Use the 'realtime' hardware option on a device that was already"
    echo "provisioned. It applies only the realtime tuning and skips the general"
    echo "setup, which must not run twice."
    exit 1
fi

hw=$1
realtime=$2

bootConfig=''
if [ -e '/boot/firmware/config.txt' ]; then
    bootConfig='/boot/firmware/config.txt'
elif [ -e '/boot/config.txt' ]; then
    bootConfig='/boot/config.txt'
else
    echo "boot config.txt file not found"
    exit 1
fi

# The kernel command line lives beside config.txt but in a separate file, and it
# is a single line. Realtime CPU isolation is set there, not in config.txt.
bootCmdline=''
if [ -e '/boot/firmware/cmdline.txt' ]; then
    bootCmdline='/boot/firmware/cmdline.txt'
elif [ -e '/boot/cmdline.txt' ]; then
    bootCmdline='/boot/cmdline.txt'
fi

# isolatedCPU is the core reserved for the haptic audio producer thread. It must
# match hapticRealtimeCPU in app/app_constants.go, or the producer will decline
# to pin itself. CPU 0 is never used because most interrupts land there.
isolatedCPU=3

# backupOnce copies a file to <file>.bak only when no backup exists yet. The
# first backup is the pristine one, so a re-run must never overwrite it with an
# already-modified copy: that would silently destroy the rollback point.
backupOnce() {
    if [ -e "$1.bak" ]; then
        echo "Backup already exists, keeping it: $1.bak"
        return 0
    fi

    if ! cp -a "$1" "$1.bak"; then
        echo "Failed to backup $1"
        exit 1
    fi

    echo "Backed up $1 to $1.bak"
}

backupConfig() {
    backupOnce ${bootConfig}
    backupOnce /etc/systemd/journald.conf
}

# appendOnce adds a line to a file only when that exact line is not present
# already, so the function can run any number of times without duplicating it.
appendOnce() {
    line="$1"
    file="$2"

    if grep -qxF "${line}" "${file}"; then
        echo "Already set in ${file}: ${line}"
        return 0
    fi

    echo "${line}" >> "${file}"
    echo "Added to ${file}: ${line}"
}

generalSetup() {
    backupConfig

    # apt-get install is already idempotent; it is a no-op when log2ram is
    # present and up to date.
    apt-get update && apt-get install -y log2ram

    appendOnce "SystemMaxUse=50M" /etc/systemd/journald.conf
    appendOnce "ForwardToSyslog=no" /etc/systemd/journald.conf
}

# addRealtimeTuning reserves one core for the haptic audio producer. It writes
# every part of the tuning that is fixed at provisioning time: the kernel
# command line, the service CPU affinity, and the irqbalance ban list. The one
# part that cannot live here is the interrupt affinity, which resets at every
# boot and is applied by support/rt-tune.sh.
#
# Read doc/realtime_tuning.md before enabling this. Measure first: on a machine
# with no underruns, isolating a core costs a quarter of the CPU and gains
# nothing.
addRealtimeTuning() {
    if [ -z "${bootCmdline}" ]; then
        echo "cmdline.txt not found, skipping realtime tuning"
        return
    fi

    if grep -q 'isolcpus=' ${bootCmdline}; then
        echo "isolcpus already set in ${bootCmdline}, leaving it alone"
        return
    fi

    cp -a ${bootCmdline} ${bootCmdline}.bak
    if [ $? -ne 0 ]; then
        echo "Failed to backup ${bootCmdline}"
        exit 1
    fi

    # nohz_full stops the periodic timer tick on the isolated core and rcu_nocbs
    # moves its RCU callbacks elsewhere. Without both, the core is reserved but
    # still interrupted, and the isolation buys far less than it appears to.
    sed -i "1s|\$| isolcpus=${isolatedCPU} nohz_full=${isolatedCPU} rcu_nocbs=${isolatedCPU}|" ${bootCmdline}
    if [ $? -ne 0 ]; then
        echo "Failed to add CPU isolation to ${bootCmdline}"
        exit 1
    fi

    addServiceCPUAffinity
    banIrqbalanceCPU

    echo "Reserved CPU ${isolatedCPU}. Reboot to apply."
}

# addServiceCPUAffinity keeps the general Simtezilo threads off the isolated
# core. CPUAffinity only sets the initial mask, so the audio producer still
# widens its own affinity back to the isolated core once it holds its realtime
# policy.
#
# This is a drop-in rather than an edit to simtezilo.service so that a package
# upgrade cannot silently drop it, and so removing the directory is a complete
# rollback.
addServiceCPUAffinity() {
    local overrideDir='/etc/systemd/system/simtezilo.service.d'
    local shared=''
    local cpuCount=$(nproc)
    local cpu=0

    while [ ${cpu} -lt ${cpuCount} ]; do
        if [ ${cpu} -ne ${isolatedCPU} ]; then
            shared="${shared}${shared:+,}${cpu}"
        fi

        cpu=$((cpu + 1))
    done

    mkdir -p "${overrideDir}"

    cat > "${overrideDir}/10-realtime.conf" <<EOF
# Generated by support/rpi-setup.sh. Do not edit by hand.
# Remove this directory to undo the CPU isolation half of the realtime tuning.
[Service]
CPUAffinity=${shared}
EOF

    systemctl daemon-reload 2>/dev/null || true

    echo "Wrote ${overrideDir}/10-realtime.conf with CPUAffinity=${shared}"
}

# banIrqbalanceCPU stops irqbalance moving interrupts back onto the isolated
# core on its next pass. Banning the core is better than masking the service:
# irqbalance keeps doing its job on the remaining cores, and the setting
# survives a package upgrade.
banIrqbalanceCPU() {
    local defaults='/etc/default/irqbalance'
    local mask=$(printf '%x' $((1 << isolatedCPU)))

    if [ ! -e ${defaults} ]; then
        echo "irqbalance not installed, skipping its ban list"
        return
    fi

    if grep -q 'IRQBALANCE_BANNED_CPULIST' ${defaults}; then
        echo "irqbalance ban list already set, leaving it alone"
        return
    fi

    cp -a ${defaults} ${defaults}.bak

    # Newer irqbalance reads the CPU list; older builds only read the hex mask.
    # Writing both keeps this correct across Raspberry Pi OS releases.
    cat >> ${defaults} <<EOF

# Added by support/rpi-setup.sh: keep interrupts off the isolated audio core.
IRQBALANCE_BANNED_CPULIST=${isolatedCPU}
IRQBALANCE_BANNED_CPUS=${mask}
EOF

    echo "Banned CPU ${isolatedCPU} in ${defaults}"
}

enableSPI() {
    if grep -qE '^dtparam=spi=on' ${bootConfig}; then
        echo "SPI already enabled in ${bootConfig}"
        return 0
    fi

    if grep -qE '^#dtparam=spi=' ${bootConfig}; then
        # Uncomment the existing setting.
        if ! sed -i 's/^#dtparam=spi=.*/dtparam=spi=on/' ${bootConfig}; then
            echo "Failed to enable SPI in ${bootConfig}"
            exit 1
        fi
    else
        # No setting to uncomment, so add one.
        appendOnce 'dtparam=spi=on' ${bootConfig}
    fi

    echo "Enabled SPI in ${bootConfig}"
}

disableHDMIAudio() {
    if ! grep -qE '^dtparam=audio=on' ${bootConfig}; then
        echo "HDMI audio already disabled in ${bootConfig}"
        return 0
    fi

    if ! sed -i 's/^dtparam=audio=on/dtparam=audio=off/' ${bootConfig}; then
        echo "Failed to disable HDMI audio in ${bootConfig}"
        exit 1
    fi

    echo "Disabled HDMI audio in ${bootConfig}"
}

addPirateAudioBasicSettings() {
    if grep -qE '^dtoverlay=hifiberry-dac' ${bootConfig}; then
        echo "Pirate Audio settings already present in ${bootConfig}"
        return 0
    fi

    # The block must land in the default section, above the [cm4] section
    # header, or it would only apply to a Compute Module 4. The brackets are
    # escaped because an unescaped [cm4] is a character class matching the
    # letters c and m and the digit 4 anywhere in the file.
    if grep -qF '[cm4]' ${bootConfig}; then
        if ! sed -i "s/^\\[cm4\\]/# Pirate Audio\\ngpio=13=op,dl\\ndtoverlay=hifiberry-dac\\n\\n[cm4]/" ${bootConfig}; then
            echo "Failed to add Pirate Audio settings to ${bootConfig}"
            exit 1
        fi
    else
        # No [cm4] section to anchor against, so append instead.
        printf '\n# Pirate Audio\ngpio=13=op,dl\ndtoverlay=hifiberry-dac\n' >> ${bootConfig}
    fi

    echo "Added Pirate Audio settings to ${bootConfig}"
}

addPirateAudioButtonConfig() {
    if grep -qE '^gpio=25=op,dh' ${bootConfig}; then
        echo "Pirate Audio button config already present in ${bootConfig}"
        return 0
    fi

    if ! grep -qE '^dtoverlay=hifiberry-dac' ${bootConfig}; then
        echo "Cannot add button config: dtoverlay=hifiberry-dac not found in ${bootConfig}"
        exit 1
    fi

    if ! sed -i "s/^dtoverlay=hifiberry-dac/dtoverlay=hifiberry-dac\\ngpio=25=op,dh\\ngpio=5=ip,dh\\ngpio=6=ip,dh\\ngpio=16=ip,dh\\ngpio=24=ip,dh/" ${bootConfig}; then
        echo "Failed to add Pirate Audio button config to ${bootConfig}"
        exit 1
    fi

    echo "Added Pirate Audio button config to ${bootConfig}"
}

case $hw in
    'none')
        generalSetup
        ;;
    'pirateaudio')
        generalSetup
        enableSPI
        disableHDMIAudio
        addPirateAudioBasicSettings
        addPirateAudioButtonConfig
        ;;
    'waveshare')
        generalSetup
        enableSPI
        ;;
    'realtime')
        # Realtime tuning only, for a device that is already provisioned.
        # generalSetup must not run twice: it appends the journald settings
        # again and overwrites the pristine backups with modified copies.
        realtime='realtime'
        ;;
    *)
        echo "Unknown hardware: $hw"
        echo "Valid options are: none, pirateaudio, waveshare, realtime"
        exit 1
        ;;
esac

if [ "${realtime}" = 'realtime' ]; then
    addRealtimeTuning
fi
