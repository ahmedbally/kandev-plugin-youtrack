package main

import (
	"github.com/kandev/kandev/pkg/pluginsdk"
	"github.com/kdlbs/kandev-plugin-youtrack/internal/plugin"
)

func main() {
	p := new(plugin.Plugin)
	plugin.StartPoller(p)
	pluginsdk.Serve(p)
}