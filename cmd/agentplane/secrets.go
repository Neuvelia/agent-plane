package main

import (
	"os"

	"agentplane/internal/adapter/envsecrets"
	"agentplane/internal/runtime"
)

// secretResolver is the one place the runner's own environment becomes a secret
// source (F3.8, ADR-0029). run and serve call it for every run, so serve reads
// the environment of its own process at the moment a run starts and keeps no
// resolved value between runs. The resolver holds no value itself.
func secretResolver() runtime.SecretResolver {
	return envsecrets.Provider{Lookup: os.LookupEnv}
}
