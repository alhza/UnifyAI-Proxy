# Code Cleanup Verification Test Script
# Verifies dead code has been removed and project still compiles/tests

param(
    [switch]$Verbose
)

$ErrorActionPreference = "Stop"
$script:testsPassed = 0
$script:testsFailed = 0

function Write-TestResult {
    param([string]$Name, [bool]$Passed, [string]$Message = "")
    if ($Passed) {
        Write-Host "  [PASS] $Name" -ForegroundColor Green
        $script:testsPassed++
    } else {
        Write-Host "  [FAIL] $Name" -ForegroundColor Red
        if ($Message) { Write-Host "         $Message" -ForegroundColor Yellow }
        $script:testsFailed++
    }
}

Write-Host ""
Write-Host "=== Code Cleanup Verification ===" -ForegroundColor Cyan
Write-Host "Verifying dead code removed and project works"
Write-Host ""

# Test 1: Verify deleted directories don't exist
Write-Host "1. Verify deleted directories" -ForegroundColor White
$deletedDirs = @(
    "internal/proxy",
    "internal/transform", 
    "internal/store"
)
foreach ($dir in $deletedDirs) {
    $exists = Test-Path $dir
    $msg = if($exists){"Directory still exists"}else{""}
    Write-TestResult "Directory $dir removed" (-not $exists) $msg
}

# Test 2: Verify no import references to deleted packages
Write-Host ""
Write-Host "2. Verify no residual import references" -ForegroundColor White
$deadImports = @(
    'internal/proxy"',
    'internal/transform"',
    'internal/store"'
)
foreach ($import in $deadImports) {
    $refs = Get-ChildItem -Path "." -Recurse -Include "*.go" -ErrorAction SilentlyContinue | 
            Select-String -Pattern $import -SimpleMatch -ErrorAction SilentlyContinue
    $hasRefs = $null -ne $refs -and $refs.Count -gt 0
    $msg = if($hasRefs){"Found $($refs.Count) references"}else{""}
    Write-TestResult "No $import references" (-not $hasRefs) $msg
}

# Test 3: go vet static analysis
Write-Host ""
Write-Host "3. Go static analysis (go vet)" -ForegroundColor White
try {
    $vetOutput = go vet ./... 2>&1
    $vetPassed = $LASTEXITCODE -eq 0
    $msg = if(-not $vetPassed){$vetOutput -join " "}else{""}
    Write-TestResult "go vet passed" $vetPassed $msg
} catch {
    Write-TestResult "go vet passed" $false $_.Exception.Message
}

# Test 4: Build check
Write-Host ""
Write-Host "4. Build check (go build)" -ForegroundColor White
try {
    $buildOutput = go build ./... 2>&1
    $buildPassed = $LASTEXITCODE -eq 0
    $msg = if(-not $buildPassed){$buildOutput -join " "}else{""}
    Write-TestResult "go build passed" $buildPassed $msg
} catch {
    Write-TestResult "go build passed" $false $_.Exception.Message
}

# Test 5: Unit tests
Write-Host ""
Write-Host "5. Unit tests (go test)" -ForegroundColor White
try {
    $testOutput = go test ./... 2>&1
    $testPassed = $LASTEXITCODE -eq 0
    $msg = if(-not $testPassed){$testOutput -join " "}else{""}
    Write-TestResult "go test passed" $testPassed $msg
} catch {
    Write-TestResult "go test passed" $false $_.Exception.Message
}

# Test 6: Verify transformer package exists and is complete
Write-Host ""
Write-Host "6. Verify transformer package completeness" -ForegroundColor White
$transformerFiles = @(
    "internal/transformer/transformer.go",
    "internal/transformer/claude.go",
    "internal/transformer/gemini.go"
)
foreach ($file in $transformerFiles) {
    $exists = Test-Path $file
    $msg = if(-not $exists){"File not found"}else{""}
    Write-TestResult "File $file exists" $exists $msg
}

# Summary
Write-Host ""
Write-Host "=== Test Results ===" -ForegroundColor Cyan
$total = $script:testsPassed + $script:testsFailed
$color = if($script:testsFailed -eq 0){"Green"}else{"Yellow"}
Write-Host "Passed: $($script:testsPassed)/$total" -ForegroundColor $color
if ($script:testsFailed -gt 0) {
    Write-Host "Failed: $($script:testsFailed)/$total" -ForegroundColor Red
    exit 1
}
Write-Host ""
Write-Host "Code cleanup verification complete!" -ForegroundColor Green
exit 0

