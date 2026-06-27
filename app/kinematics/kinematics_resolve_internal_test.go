package kinematics

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// step advances the state by one frame: it shifts Current into Last, installs the
// supplied raw derivative values and sequence/format, then runs the resolver. This
// mirrors what State.Update does around resolveDerivatives, without the telemetry
// plumbing, so the warm-up/fallback logic can be exercised in isolation.
func step(st *State, seq uint32, format string, calcTransJerk, calcTransSnap, nativeTransJerk, nativeTransSnap, calcRotJerk, calcRotSnap float64) {
	st.Last = st.Current
	st.Current = Kinematics{
		SequenceID: seq,
		Format:     format,
	}
	st.Current.SixDOFTranslationCalc.Jerk = calcTransJerk
	st.Current.SixDOFTranslationCalc.Snap = calcTransSnap
	st.Current.SixDOFTranslation.Jerk = nativeTransJerk
	st.Current.SixDOFTranslation.Snap = nativeTransSnap
	st.Current.SixDOFRotationCalc.Jerk = calcRotJerk
	st.Current.SixDOFRotationCalc.Snap = calcRotSnap

	st.resolveDerivatives()
}

// warm drives enough contiguous frames that the calculated chain is fully valid,
// returning the state and the next (unused) sequence ID.
func warm(format string, startSeq uint32) (*State, uint32) {
	st := &State{}

	seq := startSeq
	for range calcSnapWarmFrames + 1 {
		step(st, seq, format, 100, 200, 50, 80, 10, 20)
		seq++
	}

	return st, seq
}

func TestResolveSuppressedUntilCalcWarm(t *testing.T) {
	t.Parallel()

	st := &State{}

	// Fewer than calcSnapWarmFrames contiguous frames on a format without a native
	// fallback: nothing is resolved yet.
	for seq := uint32(1); seq < calcSnapWarmFrames; seq++ {
		step(st, seq, "A", 100, 200, 0, 0, 10, 20)
		require.Zero(t, st.Current.ResolvedTransJerk)
		require.Zero(t, st.Current.ResolvedRotJerk)
	}

	// The calcSnapWarmFrames'th contiguous frame: calc takes over for both domains.
	step(st, calcSnapWarmFrames, "A", 100, 200, 0, 0, 10, 20)
	require.InDelta(t, 100, st.Current.ResolvedTransJerk, 1e-9)
	require.InDelta(t, 200, st.Current.ResolvedTransSnap, 1e-9)
	require.InDelta(t, 10, st.Current.ResolvedRotJerk, 1e-9)
	require.InDelta(t, 20, st.Current.ResolvedRotSnap, 1e-9)
}

func TestResolveUsesCalcWhenWarm(t *testing.T) {
	t.Parallel()

	st, _ := warm("A", 1)

	require.InDelta(t, 100, st.Current.ResolvedTransJerk, 1e-9)
	require.InDelta(t, 200, st.Current.ResolvedTransSnap, 1e-9)
	require.InDelta(t, 10, st.Current.ResolvedRotJerk, 1e-9)
	require.InDelta(t, 20, st.Current.ResolvedRotSnap, 1e-9)
}

func TestResolveGapSuppressesOnFormatWithoutNative(t *testing.T) {
	t.Parallel()

	st, seq := warm("A", 1)

	// A gap: sequence jumps rather than incrementing by one. The spiky raw values
	// are suppressed because no source is warm and "A" has no native fallback.
	step(st, seq+5, "A", 9999, 9999, 7777, 7777, 8888, 8888)

	require.Zero(t, st.Current.ResolvedTransJerk)
	require.Zero(t, st.Current.ResolvedTransSnap)
	require.Zero(t, st.Current.ResolvedRotJerk)
	require.Zero(t, st.Current.ResolvedRotSnap)
}

func TestResolveNativeFallbackIsTranslationalOnlyThenCalc(t *testing.T) {
	t.Parallel()

	st, seq := warm("~", 1)

	// Gap on a native-capable format resets the counter.
	seq += 5
	step(st, seq, "~", 9999, 9999, 7777, 7777, 8888, 8888)
	require.Zero(t, st.Current.ResolvedTransJerk, "first post-gap frame: nothing warm")

	// Advance to exactly nativeSnapWarmFrames contiguous frames: native is warm but
	// calc is not, so the pulse is translational-only (rotational suppressed).
	for st.contiguousFrames < nativeSnapWarmFrames {
		seq++
		step(st, seq, "~", 111, 222, 55, 66, 33, 44)
	}

	require.InDelta(t, 55, st.Current.ResolvedTransJerk, 1e-9, "native jerk")
	require.InDelta(t, 66, st.Current.ResolvedTransSnap, 1e-9, "native snap")
	require.Zero(t, st.Current.ResolvedRotJerk, "rotational has no native fallback")
	require.Zero(t, st.Current.ResolvedRotSnap)

	// One more contiguous frame reaches calcSnapWarmFrames: calc takes over both
	// domains and native is no longer used.
	seq++
	step(st, seq, "~", 121, 232, 55, 66, 34, 45)
	require.InDelta(t, 121, st.Current.ResolvedTransJerk, 1e-9, "calc jerk")
	require.InDelta(t, 34, st.Current.ResolvedRotJerk, 1e-9, "calc rotational jerk")
}

func TestResolveJerkAndSnapShareOneSource(t *testing.T) {
	t.Parallel()

	st, seq := warm("~", 1)

	// During the native-fallback window, jerk and snap both come from native — never
	// a warmed jerk paired with an unwarmed (zero) snap.
	seq += 5
	step(st, seq, "~", 9999, 9999, 7777, 7777, 8888, 8888)

	for st.contiguousFrames < nativeSnapWarmFrames {
		seq++
		step(st, seq, "~", 111, 222, 55, 66, 33, 44)
	}

	require.NotZero(t, st.Current.ResolvedTransJerk)
	require.NotZero(t, st.Current.ResolvedTransSnap)
}

func TestFormatSupportsNativeEnvelope(t *testing.T) {
	t.Parallel()

	require.True(t, formatSupportsNativeEnvelope("~"))
	require.True(t, formatSupportsNativeEnvelope("B"))
	require.False(t, formatSupportsNativeEnvelope("A"))
	require.False(t, formatSupportsNativeEnvelope(""))
}
