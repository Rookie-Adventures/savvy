package model

// NotifyManagerUpgradeFn, when set, is invoked asynchronously after a
// subscription order commits, asking the manager to hot-upgrade the user's
// running container to the plan's resource tier. It is wired by the service
// package at startup (service imports model; the reverse import would form a
// cycle). nil = no-op (e.g. before service init or in unit tests that override).
//
// Failures are swallowed by the caller and only SYSLOG'd; the manager scanner
// is the safety net (漏洞1 兜底).
var NotifyManagerUpgradeFn func(userID int, upgradeGroup string) error

// NotifyManagerDowngradeFn, when set, is invoked asynchronously after a
// subscription expiry commits, asking the manager to drop the user's running
// container back to FREE with a fresh 2h free window. Same wiring/lifecycle as
// NotifyManagerUpgradeFn.
var NotifyManagerDowngradeFn func(userID int) error
