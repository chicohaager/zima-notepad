module github.com/chicohaager/zima-notepad

go 1.27.0

toolchain go1.27.4

require github.com/chicohaager/lintux-modkit v0.0.0

// Build against a sibling checkout of lintux-modkit (as zima-cron does); the CI checks out the
// commit named in .github/workflows.
replace github.com/chicohaager/lintux-modkit => ../lintux-modkit
