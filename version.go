package main

// Set at build time: -X main.release=<tag> -X main.revision=<sha>.
var (
	release  = "development"
	revision = ""
)

func versionLine() string {
	if revision == "" {
		return uiAppName + " " + release
	}
	return uiAppName + " " + release + " (" + revision + ")"
}
