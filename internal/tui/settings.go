package tui

import (
	"fmt"
	"strconv"
	"strings"

	"licode/internal/settings"
)

func settingValue(s *settings.Settings, key string) string {
	switch key {
	case "provider":
		return s.Provider
	case "model":
		return s.Model
	case "base_url":
		return s.BaseURL
	case "api_key":
		if s.APIKey != "" {
			return "********"
		}
		return ""
	case "temperature":
		return fmt.Sprintf("%.2f", s.Temperature)
	case "max_tokens":
		return strconv.Itoa(s.MaxTokens)
	case "max_iterations":
		return strconv.Itoa(s.MaxIterations)
	}
	return ""
}

func updateSettingField(s *settings.Settings, key, val string) {
	switch key {
	case "provider":
		s.Provider = val
	case "model":
		s.Model = val
	case "base_url":
		s.BaseURL = val
		s.BaseURL = strings.TrimRight(s.BaseURL, "/")
	case "api_key":
		s.APIKey = val
	case "temperature":
		fmt.Sscanf(val, "%f", &s.Temperature)
	case "max_tokens":
		fmt.Sscanf(val, "%d", &s.MaxTokens)
	case "max_iterations":
		fmt.Sscanf(val, "%d", &s.MaxIterations)
	}
}
