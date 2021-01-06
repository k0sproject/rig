package util

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateImageMap(t *testing.T) {

	t.Run("Provided list of images is properly tagged as custom image repo", func(t *testing.T) {
		images := []string{
			"docker/dtr:1.2.3",
			"docker/ucp:1.2.3",
		}
		customImageRepo := "dtr.acme.com/bear"
		expectedImageMap := map[string]string{
			"docker/dtr:1.2.3": "dtr.acme.com/bear/dtr:1.2.3",
			"docker/ucp:1.2.3": "dtr.acme.com/bear/ucp:1.2.3",
		}
		actual := GenerateImageMap(images, customImageRepo)
		require.Equal(t, expectedImageMap, actual)
	})
}
