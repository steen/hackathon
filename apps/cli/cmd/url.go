package cmd

import "os"

const DefaultURL = "ws://localhost:8080/ws"

func ResolveURL(flag string) string {
	if flag != "" {
		return flag
	}
	if env := os.Getenv("CHAT_SERVER"); env != "" {
		return env
	}
	return DefaultURL
}
