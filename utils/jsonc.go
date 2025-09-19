package utils

import "github.com/tailscale/hujson"

func ConvertJsoncToJson(jsonc []byte) []byte {
	std, err := hujson.Standardize(jsonc) // strips comments & trailing commas
	if err != nil {
		// fall back to original on parse error
		return jsonc
	}
	return std
}
