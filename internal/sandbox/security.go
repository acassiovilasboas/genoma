package sandbox

import (
	"fmt"
	"strings"
)

// SecurityChecker validates scripts before execution.
type SecurityChecker struct {
	maxScriptSize int // Max script size in bytes
}

// NewSecurityChecker creates a security checker.
func NewSecurityChecker(maxScriptSize int) *SecurityChecker {
	if maxScriptSize <= 0 {
		maxScriptSize = 512 * 1024 // 512 KB default
	}
	return &SecurityChecker{maxScriptSize: maxScriptSize}
}

// Check validates a script for security concerns before sandbox execution.
// Note: This is a basic pre-check. The real security comes from the container isolation.
func (sc *SecurityChecker) Check(script, language string) error {
	// Check script size
	if len(script) > sc.maxScriptSize {
		return fmt.Errorf("script exceeds maximum size (%d bytes > %d bytes)", len(script), sc.maxScriptSize)
	}

	// Check for empty script
	if strings.TrimSpace(script) == "" {
		return fmt.Errorf("script is empty")
	}

	// Language-specific checks
	switch language {
	case "python":
		return sc.checkPython(script)
	case "nodejs":
		return sc.checkNodeJS(script)
	default:
		return fmt.Errorf("unsupported script language: %s", language)
	}
}

// checkPython performs Python-specific security checks.
func (sc *SecurityChecker) checkPython(script string) error {
	// These are advisory warnings — actual blocking is done by container isolation.
	// We check for common escape attempts to give early feedback.
	dangerousPatterns := []struct {
		pattern string
		reason  string
	}{
		{"import ctypes", "ctypes can bypass Python safety"},
		{"__import__('os').system", "os.system shell execution"},
		{"subprocess.Popen", "subprocess execution"},
		{"import pty", "pty can spawn shells"},
	}

	for _, dp := range dangerousPatterns {
		if strings.Contains(script, dp.pattern) {
			return fmt.Errorf("potentially dangerous pattern detected: %s (%s)", dp.pattern, dp.reason)
		}
	}

	return nil
}

// checkNodeJS performs Node.js-specific security checks.
func (sc *SecurityChecker) checkNodeJS(script string) error {
	dangerousPatterns := []struct {
		pattern string
		reason  string
	}{
		{"child_process", "child process execution"},
		{"require('cluster')", "cluster module"},
		{"process.binding", "internal bindings"},
	}

	for _, dp := range dangerousPatterns {
		if strings.Contains(script, dp.pattern) {
			return fmt.Errorf("potentially dangerous pattern detected: %s (%s)", dp.pattern, dp.reason)
		}
	}

	return nil
}
