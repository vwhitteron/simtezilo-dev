package hardware

import (
	"log"

	"periph.io/x/host/v3"
)

func Init() {
	_, err := host.Init()
	if err != nil {
		log.Fatal(err)
	}
}
