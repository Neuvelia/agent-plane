# Plan: adapter Claude Code (drugi coding-agent adapter)

**Data:** 2026-11-27
**Status:** plan (bez implementacji); wymaga decyzji z sekcji 10 przed startem
**Proponowany ID:** `F1.2b` (kind `derived`, parent `F1.2`) — patrz D1
**Zweryfikowano przeciw:** Claude Code `2.1.332` (`claude --help`, natywny ELF, `env -i claude --version` działa)

## 1. Stan obecny

- Kontrakt neutralny jest gotowy i kanoniczny: `internal/agent/contract.go:15`
  (`ID`/`Detect`/`Execute` + opcjonalne `NormalizedProgressAdapter`), ADR-0009,
  ADR-0034. ADR-0034 mówi wprost: drugi adapter musi zachować ten kształt albo
  zmienić go ADR-em. Plan go **nie zmienia**.
- Jedyny realny adapter to Codex: `internal/agent/codex/` (ADR-0010, F1.2, F1.3).
  Wzorce do powtórzenia: bounded probes w `Detect` (`adapter.go:78`), task na
  stdin, prywatny katalog na wynik, `environment_refs` jako jedyne źródło env,
  snapshot Git przed/po przez `internal/gitstate`, drzewo procesów przez
  `internal/proctree` (`process_unix.go:18`), stałe kody `codex_*`.
- Wpięcie w CLI jest zakodowane na Codex: `cmd/agentplane/cli.go:135-155,245`
  (`--adapter codex --codex-bin`), remedy w `cmd/agentplane/executors.go:29-51`.
  `serve` używa `--codex-bin` tylko do detekcji (`serve.go:46,226`).
- Raport capability (F2.11, ADR-0025) już wykrywa `claude` jako `agent_cli` ze
  statusem `detected_no_adapter` (`internal/capability/capability.go:103`).
  Desktop ma teksty specyficzne dla wiersza `codex`/`agent_adapter`
  (`desktop/src/capabilities.ts:80`).
- Rejestr: brak wpisu dla Claude Code. `F1.2.out_of_scope`: „Drugi adapter jest
  P1 dopiero po stabilizacji kontraktu”. Kontrakt jest stabilny (F1.1–F1.8 done),
  więc to dobry moment, ale potrzebny jest nowy wpis + spec.

## 2. Zakres

**W zakresie**

1. Pakiet `internal/agent/claudecode` implementujący `AgentAdapter` +
   `NormalizedProgressAdapter` dla zainstalowanego CLI `claude` w trybie `-p`.
2. Detekcja (wersja, wymagane flagi), wywołanie z jawnym argv i środowiskiem,
   wynik strukturalny `{summary, artifacts}`, evidence z Git, anulowanie drzewa
   procesów, progress znormalizowany do istniejących typów zdarzeń F1.3.
3. Opt-in w CLI: `agentplane run --adapter claude --claude-bin ABSOLUTE_PATH`;
   `serve --claude-bin` tylko do detekcji (jak Codex).
4. Wiersz capability `claude` jako `agent_adapter` gdy skonfigurowany.
5. ADR, spec, AGENTS.md pakietu, wpis w rejestrze, smoke z prawdziwym CLI.

**Poza zakresem** (z celami odroczenia)

- Uruchamianie agentów z desktopu → F2.14.
- Usage/cost (`total_cost_usd`, tokeny) jako dane → F3.10 (v1 zwraca `Usage: nil`).
- Budżety (`--max-budget-usd`) jako konfiguracja grafu → F5.11.
- `secret_env` dla Agentów, keychain → F3.9 / dalsze F3.8.
- Wznawianie sesji (`--resume`), sub-agenci, MCP, pluginy, Claude w chmurze.
- Windows (fail closed jak Codex) → razem z F3.4a dla agentów.
- Wspólne SDK adapterów → F6.5.

## 3. Protokół CLI (do potwierdzenia w spike'u, krok 1)

```text
claude -p
  --output-format stream-json --verbose --input-format text
  --json-schema '<schemat {summary, artifacts}>'
  --no-session-persistence
  --permission-mode acceptEdits --permission-prompts none
  --tools "Read,Edit,Write,Glob,Grep[,Bash]"     # D4
  --strict-mcp-config --disable-slash-commands
  --setting-sources ""  | --bare  | --restricted  # D3
```

- `cmd.Dir = workspace` (Claude nie ma odpowiednika `--cd`); task, advisory
  `allowed_paths`, `context_refs` i feedback korekty idą na **stdin**, nigdy w argv.
- Mapowanie inwariantów ADR-0010 na Claude Code:

| Codex (ADR-0010) | Claude Code |
| --- | --- |
| `--ask-for-approval never` | `--permission-prompts none` (to, co by pytało, jest odrzucane) |
| `--sandbox workspace-write` | brak sandboxa OS domyślnie → D4 (sandbox Bash / brak Bash) |
| `--ignore-user-config` | `--setting-sources ""` lub `--bare` / `--restricted` → D3 |
| `--ephemeral` | `--no-session-persistence` |
| `--output-schema` + `--output-last-message` | `--json-schema`, wynik w rekordzie `result` (`structured_output`) |
| `--json` | `--output-format stream-json --verbose` |
| `--no-daemon` | nie dotyczy (brak daemona; **nie** używać `--bg`) |
| nigdy nie poszerzać sandboxa | nigdy `bypassPermissions` / `--dangerously-skip-permissions` / `--allow-dangerously-skip-permissions` |

Dodatkowo stałe env ustawiane przez adapter (nie z otoczenia):
`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`, `DISABLE_AUTOUPDATER=1`
(do potwierdzenia nazw w spike'u) oraz prywatny `HOME`/`CLAUDE_CONFIG_DIR` (D5).

## 4. Proponowana implementacja (kroki = commity)

0. **Spike (bez kodu produkcyjnego)** — `docs/validation/F1.2b-claude-code-spike.md`:
   potwierdzić na 2.1.332: kształt rekordów `stream-json` (`system/init`,
   `assistant`, `user`/`tool_result`, `result` z `structured_output`,
   `is_error`, `subtype`), zachowanie bez `HOME` (czy pisze `.claude.json`
   do cwd → fałszywe changed files!), działanie `--permission-prompts none`
   z `acceptEdits`, sandbox Bash na Linuksie (bubblewrap), maks. rozmiar
   rekordów `tool_result` przy `Read` dużych plików, auth przez `ANTHROPIC_API_KEY`
   vs OAuth. Wynik spike'u zamyka D3–D6.
1. **Spec + ADR** — `docs/features/F1.2b-claude-code-adapter.md` (z `_template.md`),
   `docs/adr/0046-claude-code-process-permissions-and-evidence.md`
   (`scripts/new-adr.sh`), wpis `F1.2b` w `docs/roadmap/features.yaml`,
   wiersz w `docs/roadmap/decisions/README.md`, `make roadmap`.
2. **Wydzielenie wspólnych helperów procesu** (D2) — R2 zabrania importu
   `codex` z `claudecode`. Przenieść bez zmiany zachowania `limitedBuffer`,
   `uniqueJSON` i `runProcess` do neutralnego pakietu (np. `internal/agent/cliproc`),
   `codex` tylko przełącza importy; testy Codex bez zmian muszą przejść.
3. **`claudecode.Adapter.Detect`** — absolutny `Binary`, plik wykonywalny,
   probe `--version` (regex `^([0-9]+\.[0-9]+\.[0-9]+\S*) \(Claude Code\)\s*$`),
   probe `--help` na wymagane flagi; 10 s/probe; Darwin/Linux, inaczej fail closed.
   `CorrectionFeedback/Cancellation/Streaming/Workspace = true`, `UsageReporting = false`.
4. **`Execute`** — walidacja workspace przez `gitstate` (root worktree z HEAD),
   snapshot przed, budowa argv/env (tylko `environment_refs` + stałe adaptera),
   prompt na stdin (ten sam układ sekcji co Codex: task, advisory scope,
   context refs, feedback jako niezaufane dane), start przez `proctree`,
   parsowanie strumienia, snapshot po i `ChangedFiles` z `observed`.
5. **Progress** (`progress.go`) — projekcja `stream-json` → zdarzenia F1.3,
   bez nowych typów zdarzeń (brak bumpu schematu):

   | Rekord Claude | Zdarzenie |
   | --- | --- |
   | `system` `init` | `status{session_started}` |
   | `assistant` blok `text` | `text_delta` (ukończony blok, nie tokeny) |
   | `assistant` blok `thinking` | **pomijany** (prywatne rozumowanie) |
   | `tool_use` `Bash` → `tool_result` | `command_started` / `command_finished` (`exit_code` nieznany = `nil`; bez treści komendy) |
   | `tool_use` `Edit`/`Write`/`MultiEdit`/`NotebookEdit` + udany wynik | `file_changed{provenance: reported}` (tylko ścieżka, potwierdzona jako wewnątrz workspace) |
   | inne `tool_use` | `tool_call_started/finished` (tylko nazwa narzędzia i status) |
   | `result` | `status{turn_completed}`; `is_error`/`subtype != success` → błąd `claude_result` |
   | nieznane / zbyt duże opcjonalne | pomijane |

   Uwaga: rekordy `user/tool_result` mogą przekraczać 1 MiB (np. `Read`).
   W odróżnieniu od Codex czytnik musi **pominąć** za duży rekord opcjonalny
   (strumieniowo, bez buforowania całości), a fail closed tylko dla za dużego
   lub błędnego rekordu `result`.
6. **Wynik** — wymagane `structured_output` z dokładnie `summary` (niepusty)
   i `artifacts` (`ref` niepuste); walidacja `uniqueJSON`; brak/zły wynik →
   `claude_result`, zero zaakceptowanych outputów. `RawMetadata`/`Usage` = nil.
7. **Błędy** — `claudecode.Error` z kodami: `claude_detection`,
   `claude_unsupported_platform`, `claude_unsupported_capability`,
   `claude_environment`, `claude_workspace`, `claude_git`, `claude_start`,
   `claude_shutdown`, `claude_cancelled`, `claude_process_exit`,
   `claude_result`, `claude_progress`. Nigdy surowy stderr, tekst błędu API
   ani treść narzędzi w zdarzeniach.
8. **CLI** — `cmd/agentplane/cli.go`: `--adapter claude --claude-bin`;
   rejestracja `claudecode.Adapter` w `agent.Executor.Adapters`; remedy w
   `executors.go` uogólnione na mapę adapter → komunikat (D7: czy wolno
   rejestrować oba adaptery naraz). `serve --claude-bin` → `capabilityDetector`
   przenosi `claude` z `AgentCLIs` do `Agents`.
9. **Desktop (minimum)** — teksty `cap.meaning.claude.*` w `en.ts`/`pl.ts`
   i gałąź w `capabilities.ts` dla `claude`/`agent_adapter` (albo D8: zostawić
   `claude` jako `agent_cli` do F2.14).
10. **Dokumentacja** — `internal/agent/claudecode/AGENTS.md`, wiersze w
    `internal/agent/AGENTS.md`, `docs/codebase-map.md`, `docs/graph-spec.md`
    (przykład `adapter: claude`), szablon `.agents/graphs/` z węzłem Claude.
11. **Smoke z prawdziwym CLI** — `docs/validation/F1.2b-claude-code-smoke.md`:
    jednorazowe repo, realna edycja, wersja CLI, run/node ID, `node_evidence`;
    plus jeden przebieg pętli korekty (F1.6) i `isolation: worktree` (F3.1).
    Bez tego F1.2b zostaje `partial` (jak reguła F1.2).

## 5. Pliki

| Plik | Powód |
| --- | --- |
| `internal/agent/claudecode/{adapter,progress,prompt,git,process_unix,process_other}.go` + testy | nowy adapter |
| `internal/agent/cliproc/*` (nazwa do D2), `internal/agent/codex/*` | wspólne helpery, tylko przeniesienie |
| `cmd/agentplane/cli.go`, `executors.go`, `serve.go`, `adapter_test.go` | opt-in i remedy |
| `internal/capability/capability.go` (+test) | `claude` jako adapter, gdy skonfigurowany |
| `desktop/src/capabilities.ts`, `desktop/src/i18n/{en,pl}.ts` (+vitest) | wiersz capability |
| `docs/features/F1.2b-…md`, `docs/adr/0046-…md`, `docs/roadmap/features.yaml`, `STATUS.md`, `decisions/README.md` | kontrakt i rejestr |
| `docs/validation/F1.2b-…md` | spike i smoke |
| AGENTS.md pakietów, `docs/codebase-map.md`, root `AGENTS.md` (sekcja fazy) | nawigacja |

## 6. Kontrakty i ADR

- Interfejs adaptera: bez zmian (ADR-0034).
- Graf: bez zmian schematu — `adapter: claude` to wartość istniejącego pola.
  Jeśli D4 wybierze konfigurowalną listę narzędzi per węzeł (`config.tools`),
  to **zmiana schematu grafu → osobny ADR** (rekomendacja: nie w v1).
- Zdarzenia: brak nowych typów, brak bumpu schematu; nowe wartości
  `diagnostic_codes` (`claude_*`) i `adapter_id = "claude"` w `node_evidence`
  — opisać w ADR-0046.
- CLI: nowe flagi `--adapter claude`, `--claude-bin` (publiczny kontrakt → ADR-0046).
- Capability API: nowy wiersz `agent_adapter` `claude`; ADR-0025 przewiduje
  przejście z `detected_no_adapter` — sprawdzić, czy wymaga noty do ADR-0025.
- Granica zaufania: Claude Code bez sandboxa OS jest **słabszy** niż Codex
  `workspace-write`. To decyzja o izolacji procesu → musi być jawna w ADR (D4).

## 7. Kryteria → testy (wstępnie)

| Kryterium | Test |
| --- | --- |
| AC1 fake CLI zmienia plik w tymczasowym repo; output i trace mają ścieżkę z Git | `claudecode/adapter_test.go::TestFakeExecutionGitEvidenceAndSafeProgress`, `cmd/agentplane/adapter_test.go::TestCLIExplicitClaudeAdapterAgentAndShell` |
| AC2 ścieżka z `tool_use` nieobecna w Git nie staje się changed path | `TestReportedPathNotInGitIsNotObserved` |
| AC3 brudny plik sprzed runu nie daje fałszywej zmiany; pliki konfiguracyjne Claude nie trafiają do workspace | `TestDirtyBaseline`, `TestPrivateConfigDirLeavesWorkspaceClean` |
| AC4 brak CLI / zły workspace / za stara wersja → błędy węzła bez paniki, remedy z `--claude-bin` | `TestDetectMissingAndUnsupported`, `executor_test.go` |
| AC5 anulowanie z procesem potomnym (np. background Bash) nie zostawia procesów | `process_test.go::TestOwnedChildCancellationAndOrdinaryExit` |
| AC6 `thinking`, treść narzędzi, stderr i tekst taska nie trafiają do zdarzeń | `progress_test.go::TestPrivateRecordsNeverProjected` |
| AC7 za duży opcjonalny rekord jest pomijany; zły/za duży `result` → `claude_result` bez outputów | `progress_test.go`, `adapter_test.go` |
| AC8 argv nigdy nie zawiera flag bypass; env = refs + stałe adaptera | `TestArgvAndEnvironment` |
| AC9 pętla korekty przekazuje feedback na stdin | `TestCorrectionFeedbackOnStdin` |
| AC10 smoke z prawdziwym CLI | `docs/validation/F1.2b-claude-code-smoke.md` |
| AC11 Shell i Codex bez zmian; `make check`, `go test -race ./...` | istniejące testy |

Fake CLI: skrypt shell w `testdata`, emitujący nagrane (z spike'u, zredagowane)
rekordy `stream-json`, jak w testach Codex.

## 8. Rejestr

Nowy wpis `F1.2b` (`kind: derived`, `parent: F1.2`, `phase: 1`, `priority: P1`,
`owner_package: adapters`, `depends_on: [F1.1, F1.2, F1.3, F1.6]`,
`decisions: [ADR-0046]`, `out_of_scope` z sekcji 2). Status `in_progress` od
kroku 1, `done` dopiero po smoke'u. Zmiana `F1.2.out_of_scope` — tylko nota
wskazująca na F1.2b.

## 9. Ryzyka

- **Brak sandboxa OS** dla Bash w Claude Code — największe ryzyko bezpieczeństwa (D4).
- **Zapisy konfiguracji do workspace** przy braku `HOME` → fałszywe evidence
  albo zanieczyszczenie repo (D5, test AC3).
- **Rozmiar rekordów `stream-json`** — `tool_result` bywa ogromny; czytnik
  musi być strumieniowy i ograniczony.
- **Dryf protokołu** — `stream-json` nie jest formalnie wersjonowany; detekcja
  sprawdza flagi, ale nie kształt rekordów → wymagane `structured_output`
  i fail closed; min. wersja w ADR.
- **Instalacje npm** (`claude` jako skrypt Node z `#!/usr/bin/env node`)
  nie ruszą z pustym `PATH` → jasny błąd `claude_detection` z remedy (D6).
- **Ruch sieciowy** — model jest zdalny z definicji (jak Codex); wyłączyć
  WebFetch/WebSearch, MCP, telemetrię i autoupdater.
- **Procesy w tle** (Bash `run_in_background`) — w grupie procesów, sprzątane
  po wyjściu lidera; `setsid` poza kontraktem (jak ADR-0010).
- **Koszt testów** — realny smoke wymaga płatnego auth; CI używa wyłącznie fake CLI.

## 10. needs_decision

- **D1 ID i rodzaj wpisu:** `F1.2b` (derived od F1.2) — rekomendowane; alternatywa
  nowy `F1.9`. Adapter ID: `claude` (zgodny z id wiersza capability z ADR-0025) —
  rekomendowane; alternatywa `claude-code` (jak w `full-architecture-plan.md`).
- **D2 Wspólne helpery:** wydzielić `runProcess`/`limitedBuffer`/`uniqueJSON` do
  neutralnego pakietu (rekomendowane, R2 + brak duplikacji) czy skopiować.
- **D3 Izolacja konfiguracji:** `--bare` (bez hooków, CLAUDE.md, pluginów,
  keychaina; auth tylko `ANTHROPIC_API_KEY`) + `--strict-mcp-config` —
  rekomendowane dla v1; alternatywy: `--setting-sources ""` (zachowuje OAuth),
  `--restricted` (dodatkowo blokada zapisu do plików git/ustawień).
  Konsekwencja `--bare`: projektowe CLAUDE.md nie jest czytane automatycznie.
- **D4 Bash i sandbox:** (a) bez Bash w v1 — tylko narzędzia plikowe ograniczone
  do workspace; (b) Bash tylko z sandboxem Claude Code (`--settings` z
  `sandbox.enabled`), fail closed gdy niedostępny — rekomendowane;
  (c) Bash bez sandboxa za jawną flagą CLI. Agent bez testów/kompilacji jest
  mało użyteczny, więc (b), z (a) jako fallbackiem do czasu potwierdzenia w spike'u.
- **D5 HOME / katalog konfiguracji:** adapter tworzy prywatny tymczasowy
  `HOME`/`CLAUDE_CONFIG_DIR` poza workspace i usuwa go po runie (rekomendowane)
  vs przekazanie przez `environment_refs`.
- **D6 Auth:** `ANTHROPIC_API_KEY` przez `environment_refs` (zgodne z D3/`--bare`)
  vs OAuth subskrypcji (wymaga prawdziwego `HOME` → sprzeczne z D5).
- **D7 Wiele adapterów w jednym runie:** pozwolić na `--adapter codex --adapter claude`
  (mapa w `Executor` to już wspiera) czy pojedynczy wybór w v1.
- **D8 Desktop:** zmienić wiersz `claude` na `agent_adapter` z tekstami teraz
  czy zostawić do F2.14.
- **D9 Usage:** zwracać tokeny/koszt z rekordu `result` już teraz (kontrakt ma
  `UsageMetadata`) czy `nil` do F3.10 — rekomendowane `nil`.

## 11. Definition of done

`go test ./internal/agent/...`, `go test -race ./internal/agent/... ./cmd/agentplane/...`,
vitest desktopu (jeśli D8 = teraz), `make roadmap`, `make check`, smoke z
prawdziwym CLI opisany w `docs/validation/`, przegląd diffu przez osobną sesję
(`prompts/review.md`).
