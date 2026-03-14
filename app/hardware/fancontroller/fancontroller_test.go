package fancontroller_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/fancontroller"
)

func TestNewAppliesDefaultOptions(t *testing.T) {
	t.Parallel()

	// arrange
	options := fancontroller.Options{}

	// act
	client := fancontroller.New(options)

	// assert
	require.NotNil(t, client)
}

func TestSetFanDutyRangeValidation(t *testing.T) {
	t.Parallel()

	// arrange
	client := fancontroller.New(fancontroller.Options{})
	ctx := context.Background()

	// act
	_, err := client.SetFanDuty(ctx, 101)

	// assert
	require.Error(t, err)
}
