package task

import (
	"crypto/rand"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
)

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var embeddedIDs = regexp.MustCompile(`(?i)T-[0-9]{8}-[0-9A-HJKMNP-TV-Z]{8}`)
var draftIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func ValidDraftID(value string) bool { return draftIDPattern.MatchString(value) }

func CollectIDs(data []byte, used map[string]bool) {
	for _, value := range embeddedIDs.FindAllString(string(data), -1) {
		used[strings.ToUpper(value)] = true
	}
}

func NewUniqueID(now time.Time, used map[string]bool) (string, error) {
	return uniqueID(now, used, rand.Reader)
}

func uniqueID(now time.Time, used map[string]bool, source io.Reader) (string, error) {
	for attempt := 0; attempt < 32; attempt++ {
		var entropy [8]byte
		if _, err := io.ReadFull(source, entropy[:]); err != nil {
			return "", err
		}
		for i, b := range entropy {
			entropy[i] = crockford[int(b)&31]
		}
		value := "T-" + now.UTC().Format("20060102") + "-" + string(entropy[:])
		if !used[value] {
			return value, nil
		}
	}
	return "", errors.New("task ID collision retry limit exceeded")
}

func ValidID(value string) bool {
	if !taskIDPattern.MatchString(value) {
		return false
	}
	_, err := time.Parse("20060102", value[2:10])
	return err == nil
}
