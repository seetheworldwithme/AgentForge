# smoke.ps1 — end-to-end smoke test for the agent-rust core service.
#
# Usage:
#   1. Start the core in one terminal:
#        make run
#   2. Capture the printed port, then:
#        ./scripts/smoke.ps1 -BaseUrl http://127.0.0.1:<port> -ApiKey sk-...
#
# Optional: -KbFile path\to\a.txt uploads a doc and waits for it to ingest.

param(
  [Parameter(Mandatory)][string]$BaseUrl,
  [Parameter(Mandatory)][string]$ApiKey,
  [string]$ChatModel = "gpt-4o-mini",
  [string]$EmbedModel = "text-embedding-3-small",
  [string]$KbFile
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

function Json($obj) { $obj | ConvertTo-Json -Compress }

Write-Host "==> healthz"
Invoke-RestMethod "$BaseUrl/healthz"

Write-Host "`n==> create provider"
$prov = Invoke-RestMethod "$BaseUrl/api/providers" -Method Post `
  -ContentType "application/json" `
  -Body (Json @{ name="smoke"; base_url="https://api.openai.com/v1"; api_key=$ApiKey;
                 chat_model=$ChatModel; embed_model=$EmbedModel; is_default=$true })
$prov | Format-List
if (-not $prov.id) { throw "provider creation failed" }

Write-Host "`n==> create session"
$sess = Invoke-RestMethod "$BaseUrl/api/sessions" -Method Post `
  -ContentType "application/json" `
  -Body (Json @{ title="smoke"; provider_id=$prov.id; tools_enabled=$false })
$sess | Format-List

Write-Host "`n==> chat (streamed)"
$resp = Invoke-WebRequest "$BaseUrl/api/sessions/$($sess.id)/chat" -Method Post `
  -ContentType "application/json" -Body (Json @{ message="say hi in one word" }) `
  -TimeoutSec 30
Write-Host $resp.Content

if ($KbFile) {
  Write-Host "`n==> create KB"
  $kb = Invoke-RestMethod "$BaseUrl/api/kb" -Method Post `
    -ContentType "application/json" -Body (Json @{ name="smoke-kb"; embed_provider_id=$prov.id })
  $kb | Format-List

  Write-Host "`n==> upload $KbFile"
  $form = @{ file = Get-Item $KbFile }
  $up = Invoke-RestMethod "$BaseUrl/api/kb/$($kb.id)/documents" -Method Post -Form $form
  $up | Format-List

  Write-Host "`n==> waiting for ingest…"
  for ($i = 0; $i -lt 20; $i++) {
    Start-Sleep -Seconds 2
    $docs = Invoke-RestMethod "$BaseUrl/api/kb/$($kb.id)/documents"
    $docs | Format-Table
    if ($docs[0].status -ne "processing") { break }
  }
}

Write-Host "`n==> smoke OK"
