package main

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	telemetry_client "github.com/zetetos/gt-telemetry"
)

const durationMax = int32(math.MaxInt32)

func main() {
	discordToken := "MTIwNzI4NDIyOTU2MjQzMzU4Ng.GUdQBf.T8s9FWsXUI_xP2Z7OrpELVQ4YVYikMfZ6SEASQ"
	dg, err := discordgo.New("Bot " + discordToken)
	if err != nil {
		fmt.Println("Error creating Discord session: ", err)
		return
	}

	// Open a websocket connection to Discord and begin listening.
	err = dg.Open()
	if err != nil {
		fmt.Println("error opening connection,", err)
		return
	}

	dg.AddHandler(ready)

	gt, err := telemetry_client.NewGTClient(telemetry_client.GTClientOpts{})
	if err != nil {
		fmt.Println("Error creating GT client: ", err)
		os.Exit(1)
	}

	go gt.Run()

	lastLapNotified := time.Duration(0)
	bestLap := time.Duration(durationMax)
	for {
		lastLap := gt.Telemetry.LastLaptime()
		currentLap := uint16(gt.Telemetry.CurrentLap())
		lapRecord := gt.Telemetry.BestLaptime()
		totalLaps := gt.Telemetry.RaceLaps()

		if lastLap != lastLapNotified {
			if currentLap < 1 || lastLap < time.Duration(0) {
				if bestLap != time.Duration(durationMax) {
					fmt.Println("New race started")
					bestLap = time.Duration(durationMax)
					lastLapNotified = lastLap
				}

				continue
			}

			fmt.Printf("\nLaptime: %v\nBest Lap: %v\nLap: %d of %d\n", lastLap, lapRecord, currentLap, totalLaps)

			bestLapAnnounce := ""
			if currentLap == 2 {
				bestLap = lastLap
			} else {
				if lastLap < bestLap {
					bestLapAnnounce = ", lap record"
					bestLap = lastLap
				}
			}

			lapAnnounce := fmt.Sprintf(", lap %d", currentLap)
			if currentLap > totalLaps {
				lapAnnounce = ", race complete"
			} else if currentLap == totalLaps {
				lapAnnounce = ", final lap"
			} else if currentLap >= totalLaps-4 {
				plural := "s"
				if currentLap == totalLaps {
					plural = ""
				}
				lapAnnounce = fmt.Sprintf(", %d lap%s remaining", totalLaps-currentLap+1, plural)
			}

			announce := announceLaptime(lastLap) + bestLapAnnounce + lapAnnounce

			fmt.Println(announce)

			// _, err := dg.ChannelMessageSendTTS("1146735829910306902", announce)
			// if err != nil {
			// 	fmt.Println(err.Error())
			// }

			cmd := exec.Command("/usr/bin/say", "-v", "Daniel", "-r", "190", announce)
			err := cmd.Run()
			if err != nil {
				fmt.Println(err.Error())
			}

			lastLapNotified = lastLap
		}
		time.Sleep(1090 * time.Millisecond)
	}

	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	// Cleanly close down the Discord session.
	dg.Close()

}

func ready(s *discordgo.Session, event *discordgo.Event) {
	err := s.UpdateWatchStatus(0, "Gran Turismo 7")
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	fmt.Println("Bot status updated")
}

func announceLaptime(lapTime time.Duration) string {
	minutes := int(lapTime.Minutes())
	lapTime = lapTime - (time.Duration(minutes) * time.Minute)

	seconds := int(lapTime.Seconds())
	lapTime = lapTime - (time.Duration(seconds) * time.Second)

	milliseconds := int(lapTime.Milliseconds())

	minutesStr := fmt.Sprintf("%d", minutes)
	secondsStr := fmt.Sprintf("%02d", seconds)
	millisecondsStr := fmt.Sprintf("%03d", milliseconds)

	fmt.Printf("%s:%s.%s\n", minutesStr, secondsStr, millisecondsStr)

	return pronounceTime(minutesStr, secondsStr, millisecondsStr)
}

func pronounceTime(minutes string, seconds string, milliseconds string) string {
	announce := []string{}

	announce = append(announce, minutes)
	announce = append(announce, "minutes")

	announce = append(announce, seconds)
	announce = append(announce, "point")

	for _, r := range milliseconds {
		rune := string(r)

		if rune == "0" {
			rune = "oh"
		}

		announce = append(announce, rune)
	}

	return strings.Join(announce, " ")
}
