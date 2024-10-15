package audio

type AudioBuffer struct {
	SlotSize int
	Slots    int
	Buffer   []float64
}

func NewAudioBuffer(slotSize int, slots int) AudioBuffer {
	buffer := AudioBuffer{
		SlotSize: slotSize,
		Slots:    slots,
		Buffer:   make([]float64, slotSize*slots),
	}

	buffer.ClearBuffer()

	return buffer
}

func (b *AudioBuffer) ClearBuffer() {
	for i := 0; i < len(b.Buffer); i++ {
		b.Buffer[i] = 0
	}
}

func (b *AudioBuffer) GetLength() int {
	return len(b.Buffer)
}

func (b *AudioBuffer) ShiftBuffer(slots int) {
	b.shiftBuffer2(slots)
}

func (b *AudioBuffer) shiftBuffer1(slots int) {
	offset := slots * b.SlotSize

	for i := 0; i < offset-1; i++ {
		b.Buffer[i+offset] = b.Buffer[i]
	}
}

func (b *AudioBuffer) shiftBuffer2(slots int) {
	bufferMax := (b.SlotSize * b.Slots) - 1
	offset := slots * b.SlotSize

	for i := bufferMax - offset; i >= 0; i-- {
		b.Buffer[i+offset] = b.Buffer[i]
	}
}

func (b *AudioBuffer) shiftBuffer3(slots int) {
	bufferMax := (b.SlotSize * b.Slots) - 1
	offset := slots * b.SlotSize

	for i := 0; i <= bufferMax; i++ {
		if i < bufferMax-offset {
			b.Buffer[i] = b.Buffer[i+offset]
		} else {
			b.Buffer[i] = 0
		}
	}
}
