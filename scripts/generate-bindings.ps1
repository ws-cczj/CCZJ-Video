$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $PSScriptRoot '..')
try {
    # beta.30 can emit invalid bindings and exit zero when Go type checking warns.
    go test -run '^$' ./app/service
    if ($LASTEXITCODE -ne 0) { throw 'Go service does not compile; bindings were not regenerated.' }
    $bindingOutput = & wails3 generate bindings -clean=false 2>&1
    $bindingExit = $LASTEXITCODE
    $bindingOutput | Write-Output
    if ($bindingExit -ne 0 -or ($bindingOutput -match 'WARNING')) {
        throw 'Binding generation failed or emitted warnings. Fix the Go declarations before using the output.'
    }
    node scripts/gen-request-adapter.mjs
    if ($LASTEXITCODE -ne 0) { throw 'Classify new request bindings before regenerating the adapter.' }
} finally { Pop-Location }
