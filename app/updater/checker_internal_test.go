package updater

import (
	"crypto/x509"
	"errors"
	"testing"
)

func TestIsClockSkewError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "expired certificate",
			err:  x509.CertificateInvalidError{Reason: x509.Expired},
			want: true,
		},
		{
			name: "other certificate invalid reason",
			err:  x509.CertificateInvalidError{Reason: x509.NotAuthorizedToSign},
			want: false,
		},
		{
			name: "plain error",
			err:  errors.New("boom"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := isClockSkewError(tt.err); got != tt.want {
				t.Errorf("isClockSkewError() = %v, want %v", got, tt.want)
			}
		})
	}
}
