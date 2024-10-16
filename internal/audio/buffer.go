package audio

import "sync"

type AudioBuffer struct {
	SlotSize int
	Slots    int
	Buffer   []float64

	mu sync.Mutex
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
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := 0; i < len(b.Buffer); i++ {
		b.Buffer[i] = 0
	}
}

func (b *AudioBuffer) GetLength() int {
	return len(b.Buffer)
}

func (b *AudioBuffer) Write(samples []float64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := 0; i < len(samples); i++ {
		b.Buffer[i] = (b.Buffer[i] + samples[i]) * 0.66
	}
}

func (b *AudioBuffer) Read(length int) []float64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	samples := make([]float64, length)

	for i := 0; i < length; i++ {
		samples[i] = b.Buffer[i]
	}

	return samples
}

func (b *AudioBuffer) ShiftBuffer(samples int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	bufferMax := len(b.Buffer) - samples

	for i := 0; i < bufferMax; i++ {
		b.Buffer[i] = b.Buffer[i+samples]
	}
}

func (b *AudioBuffer) ShiftBufferSlots(slots int) {
	b.mu.Lock()
	defer b.mu.Unlock()

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
