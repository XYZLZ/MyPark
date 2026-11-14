package lots_test

import (
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/lots"
)

func TestMapURL(t *testing.T) {
	lat, lng := 18.4735, -69.8849
	tests := []struct {
		name     string
		lat, lng *float64
		want     string
	}{
		{"address when no coordinates", nil, nil,
			"https://www.google.com/maps/search/?api=1&query=Calle+Arzobispo+Meri%C3%B1o+No.+386%2C+Zona+Colonial%2C+Santo+Domingo"},
		{"coordinates when both are set", &lat, &lng,
			"https://www.google.com/maps/search/?api=1&query=18.4735%2C-69.8849"},
		{"address when only one is set", &lat, nil,
			"https://www.google.com/maps/search/?api=1&query=Calle+Arzobispo+Meri%C3%B1o+No.+386%2C+Zona+Colonial%2C+Santo+Domingo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lots.MapURL(tt.lat, tt.lng, "Calle Arzobispo Meriño No. 386, Zona Colonial, Santo Domingo")
			if got != tt.want {
				t.Errorf("MapURL = %q, want %q", got, tt.want)
			}
		})
	}
}
