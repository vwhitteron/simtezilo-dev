package hardware

import (
	"log"

	"periph.io/x/host/v3"
)

func Init() {
	if _, err := host.Init(); err != nil {
		log.Fatal(err)
	}
}
