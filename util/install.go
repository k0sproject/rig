package util

import (
	"fmt"
	"io/ioutil"
	"strings"
)

// Shared install utility between install phases for different products

// SetupLicenseFile reads the license file and returns a license string command
// flag to be used with MSR and MKE installers
func SetupLicenseFile(licenseFilePath string) (string, error) {
	license, err := ioutil.ReadFile(licenseFilePath)
	if err != nil {
		return "", err
	}
	licenseFlag := fmt.Sprintf("--license '%s'", string(license))
	return licenseFlag, nil
}

// GenerateImageMap returns a new map of original --> custom repo images for the
// given slice of images associated with customImageRepo
func GenerateImageMap(images []string, customImageRepo string) map[string]string {
	imageMap := make(map[string]string, len(images))
	for index, i := range images {
		newImage := strings.Replace(i, "docker/", fmt.Sprintf("%s/", customImageRepo), 1)
		imageMap[i] = newImage
		images[index] = newImage
	}
	return imageMap
}
