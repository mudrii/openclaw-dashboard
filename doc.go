// Package dashboard is the root-package facade for the OpenClaw metrics
// dashboard: a zero-dependency Go HTTP server with an embedded SPA frontend.
//
// Logic lives in the internal/app<domain> packages (appconfig, appruntime,
// appchat, apprefresh, appserver, appsystem, appservice). This root package
// holds thin wrapper functions and type aliases that re-export those internal
// APIs, plus the CLI entrypoint (Main) and service-subcommand dispatch. New
// features add logic under internal/ first, then a root-level wrapper here.
package dashboard
