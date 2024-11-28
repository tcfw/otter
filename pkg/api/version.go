package api

type Version struct {
	Version    string `json:"version"`
	CommitHash string `json:"commitHash"`
	BuildTime  string `json:"buildTime"`
}
