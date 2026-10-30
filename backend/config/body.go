package config

import "fmt"

const DefaultRequestBodyBytes int64 = 32 << 20

func ValidateRequestBodyLimit(limit int64) error {
	if limit < 0 || limit > 1<<30 {
		return fmt.Errorf("maxBodyBytes must be between 0 and 1 GiB")
	}
	return nil
}
