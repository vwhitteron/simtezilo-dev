package internal

import (
	"fmt"
	"runtime"

	"github.com/grafana/pyroscope-go"
)

type PyroscopeProfiler struct {
	running  bool
	endpoint string
	tags     map[string]string
	profiler *pyroscope.Profiler
}

func NewPyroscopeProfiler(endpoint string, tags map[string]string) (*PyroscopeProfiler, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("profiler endpoint is required")
	}

	return &PyroscopeProfiler{
		endpoint: endpoint,
		tags:     tags,
		profiler: nil,
	}, nil
}

func (p *PyroscopeProfiler) Endpoint() string {
	return p.endpoint
}

func (p *PyroscopeProfiler) Start() error {
	runtime.SetMutexProfileFraction(5)
	runtime.SetBlockProfileRate(5)

	profiler, err := pyroscope.Start(pyroscope.Config{
		ApplicationName: "gt-pi",
		ServerAddress:   p.endpoint,
		Logger:          pyroscope.StandardLogger,
		// Logger: nil, FIXME
		Tags: p.tags,
		ProfileTypes: []pyroscope.ProfileType{
			pyroscope.ProfileCPU,
			pyroscope.ProfileAllocObjects,
			pyroscope.ProfileAllocSpace,
			pyroscope.ProfileInuseObjects,
			pyroscope.ProfileInuseSpace,
			pyroscope.ProfileGoroutines,
			pyroscope.ProfileMutexCount,
			pyroscope.ProfileMutexDuration,
			pyroscope.ProfileBlockCount,
			pyroscope.ProfileBlockDuration,
		},
	})
	if err != nil {
		return fmt.Errorf("pyroscope.Start(): %v", err)
	}

	p.profiler = profiler

	return nil
}

func (p *PyroscopeProfiler) Shutdown() error {
	if p.profiler == nil {
		return nil
	}

	err := p.profiler.Stop()
	if err != nil {
		return fmt.Errorf("pyroscope.Stop(): %v", err)
	}

	p.profiler = nil

	return nil
}
