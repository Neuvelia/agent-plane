# Plan pracy dla Macieja (smaciej911-commits) - 2026-11-18

Stan wyjściowy: `main` @ `4578233`, event schema **11**. Źródła: otwarte PR-y
(#124-#128), otwarte issue (#24, #59, #83, #84, #85), specyfikacje w
`docs/features/`, plany w `docs/superpowers/plans/`, ADR 0024-0031.

## 1. Co już jest zrobione i co jest w toku (nie dublować)

| Obszar | Stan | Kto |
| --- | --- | --- |
| F3.1 izolacja worktree | zamknięte (#107, #110, #112, #123) | Maciej + Kacper |
| F3.2 durable run state, zadania 1-4 | na `main` (#121) | Maciej |
| F3.2 zadania 5-9 | **otwarte PR-y #124-#128** (crash test, snapshot, prune, API/desktop, zamknięcie) | Kacper |
| F2.3 E5a.1 (operacje silnika) | na `main` (#120) | Maciej |
| F2.2 loop+gate | #119 | Maciej |
| Specyfikacje i plany: F3.4, F3.8, F3.9, F2.8b | spec + plan na `main`, **kodu brak** | Maciej |

Zasada: Maciej **nie dotyka** `internal/store`, `serve` ani desktopowych
plików z PR-ów #124-#128 do czasu ich zmergowania (konflikty).

## 2. Kolejność priorytetów

1. **Blok A - szybkie porządki (0,5-1 dnia).** Zamknięcie zaległych issue.
2. **Blok B - F3.4 Phase A + B (Anulowanie i timeouty) ~ 5-7 dni.** Największa wartość, odblokowuje F3.9.
3. **Blok C - F3.8 Secrets (T1-T5) ~ 4-5 dni**, równolegle po Bloku B/T5.
4. **Blok D - F2.8b attempt diff (engine) ~ 4-5 dni** - w razie blokady B/C.
5. **Blok E - F2.3 E5a.2/E5a.3 (UI) ~ 4 dni** - praca frontendowa jako przeplatka.
6. **Blok F - F3.9 execution policy** - dopiero po B(T1-T3) i C; osobny sprint.
7. **Poza zakresem teraz:** F3.3 (brak specu), F3.5-F3.7, F3.10-F3.11, REL1 (wymaga rename `agentplane` -> `tarmind`, decyzja Kacpra), Phase 4+.

Uzasadnienie: F3.4 `proctree` jest zależnością F3.9 (T9-T11); F3.3 Resume wymaga
domkniętego F3.2 (PR #128) i nie ma specu - wymaga najpierw spec+ADR.

## 3. Konflikt numerów event schema (zdecydować PRZED kodem)

| Funkcja | Numer schema | Uwagi |
| --- | --- | --- |
| `main` | 11 | F3.1 |
| F2.8b `attempt_diff` | 12 | plan F2.8b, pyt. 1 |
| F3.4 reasons/timeouts | 13 (proponowany) | T5 |
| F3.8/F3.9 | 14 (proponowany) | T1 |

Numer ustala się przy merge; kto merguje później, czyta
`events.SchemaVersion` z `origin/main` i podbija. Każdy PR z bumpem grepuje
literał `11`/`12`/`13` w testach. Test "wyższy niż wspierany" w F3.2 ma
liczyć `SchemaVersion + 1`, nie literał (poinformować Kacpra).

## 4. Blok A - porządki i issue (start od tego)

| # | Task | Akceptacja |
| --- | --- | --- |
| A1 | **#83** - F2.2: loop+gate załatwione w #119, human-node: spec F2.2 jest "all acceptance criteria proven" | Zweryfikować w `docs/features/F2.2-node-inspector.md` brak `[ ]`; jeśli tak - zamknąć #83 z linkiem do dowodu (testy human-node) |
| A2 | **#85** - F2.11 desktop wszedł w #97, ale issue otwarte, a w spec zostały 3 niezaznaczone kryteria (`grep '\[ \]'`) | Przejść 3 kryteria: albo test/e2e, albo zaznaczyć. Dopisać weryfikację Linux. ADR-0025 `proposed` -> `accepted` po domknięciu. `docs/features/README.md` (F2.11 "desktop pending" - nieaktualne) |
| A3 | **#59** - drift statusów | Poprawić `docs/features/README.md`, `AGENTS.md` (opis fazy), `docs/codebase-map.md` (`internal/graph/edit`, `desktop/src/editor`); ADR-0023 zostaje zgodny z prawdą. Po #121/#120 dodać F3.2/F2.3 |
| A4 | **#24** - macOS | Wymaga maszyny macOS - **zablokowane**, nie w tym sprincie bez dostępu; zostawić z komentarzem |
| A5 | Ustalić z Kacprem odpowiedzi na 10 pytań z planu F2.8b (#84) | Odpowiedzi w komentarzu issue; bez nich F2.8b T3+ nie startuje |

Zakres A1-A3 to docs/tests, bez zmian kontraktów. DoD: `make check` zielone,
brak zmian poza wymienionymi plikami.

## 5. Blok B - F3.4 Anulowanie i timeouty (spec: `F3.4-cancellation-and-timeouts.md`, plan: `2026-11-17-f3-4-cancellation-timeouts.md`, ADR-0028)

Cel: jeden kontrakt zakończenia drzewa procesów dla wszystkich procesów
potomnych, powody anulowania/timeouty z eventami. Zależności: F3.1 jest już na
`main`, więc T7/T8 są odblokowane (nota w planie o czekaniu na F3.1 T3/T4 jest
nieaktualna).

| Task | Zakres | Główne pliki | Testy / akceptacja |
| --- | --- | --- | --- |
| B1 (T1) | `internal/proctree` (Unix) + helper startu procesu; tymczasowy plik Windows żeby moduł się budował | nowy pakiet | testy W3 (idempotentny cancel), zabicie całego drzewa, brak sieroty, `-race` |
| B2 (T2) | Shell na `proctree` | `internal/shell/executor.go` | przypadek: proces-dziecko żyje po anulowaniu = czerwony przed zmianą |
| B3 (T3) | Codex na tym samym helperze | `internal/agent/codex` | jw. dla Codex fake binary |
| B4 (T4) | Command Gate + helpery Git | `internal/gate`, `internal/gitstate` | zamknięcie drzewa gate/git |
| B5 (T5) | Typy domenowe i event schema 13 | `internal/domain/events`, `run` | round-trip serializacji, starsze streamy dekodują |
| B6 (T6) | Powierzchnia grafu: `timeout_seconds`, `limits.run_timeout_seconds` | `internal/graph`, `internal/fields` | walidacja, deskryptory pól, błędy z lokalizacją YAML |
| B7 (T7) | Runtime: przyczyny, deadline, klasyfikacja, bound porzucenia | `internal/runtime` | testy ze sztucznym zegarem, bez `sleep` |
| B8 (T8) | CLI i `serve` (`--run-timeout`, per-run cancel) | `cmd/agentplane` | integracja CLI |
| B9 (T9) | Dowód na zbudowanym binarium | testy cmd | `kill`/timeout end-to-end |

Kolejność: B1->B2->B3->B4; B5, B6 równolegle z fazą A; B7 wymaga B2-B5.
PR-y małe (jedno zadanie = jeden PR, tak jak #107/#110/#112).

**Decyzje do potwierdzenia przed B7** (z planu, sekcja "Decisions to confirm"):
zakres `sibling_failed`, brak HTTP cancel endpoint, pole `limits` w
`run_started`, `Leaked` na macOS, wartość `CancelBound`, strategia Windows.
Rekomendacje są w planie; Maciej wysyła listę Kacprowi **przed** otwarciem B7.

Faza C (Windows, T10-T13) - **nie w tym sprincie**; po F3.8/F3.9 i naprawie #87
(Windows tests) - patrz Blok F.

## 6. Blok C - F3.8 Secrets References (spec `F3.8-secrets-references.md`, plan `2026-11-17-f3-8-f3-9-secrets-and-policy.md`, ADR-0029)

Najpierw wyłącznie F3.8 (T1-T5) - F3.9 nie startuje do jego domknięcia.

| Task | Zakres | Akceptacja |
| --- | --- | --- |
| C1 (T1) | Eventy, rodzaje checków, bump schema | schema 14 (do potwierdzenia przy merge) |
| C2 (T2) | Domena secrets + addytywny schemat grafu + pola | walidacja kolizji `E_SECRET_ENV_COLLISION`, brak sekretów w grafie |
| C3 (T4a) | Rozszerzenie zbioru redakcji + kodowania | `TestRedactorBoundsRejectOversizedSet` (64 sekrety, 16 KiB, 16 form) |
| C4 (T3) | Pipeline runtime, port resolvera | **po** F3.1 (już jest); nowy plik, jedna linia wywołania w `preflightRun` |
| C5 (T4b) | Dostarczanie do Shell/Agent | wartość nigdy w eventach/logach |
| C6 (T5) | Provider, CLI/serve, skan store, detekcja | `TestServeNeverReturnsSecretValues`, `TestPreflightRedactionBoundFailsLoudly` |

Uwagi: skan store jest rekurencyjny - obejmie pliki F3.2 bez zmian; po T1
poinformować Kacpra o teście "newer schema". Decyzje D2-D5 w planie zostały
podjęte (leader w imieniu product ownera) - zespół może je zrewidować;
Maciej zgłasza sprzeciw przed T3.

## 7. Blok D - F2.8b attempt diff (issue #84; spec `F2.8-attempt-compare.md`, plan `2026-11-17-f2-8b-attempt-diff-capture.md`, ADR-0026)

| Task | Zakres | Start |
| --- | --- | --- |
| D1 (T1) | Czysty diff liniowy `internal/domain/diff` | od razu (nie wymaga odpowiedzi) |
| D2 (T2) | Event `attempt_diff`, schema 12 | od razu; sprawdzić `SchemaVersion` na `main` |
| D3 (T3) | Adapter capture (`internal/adapter/attemptdiff`) | **po** odpowiedziach Kacpra Q3-Q9 |
| D4 (T4) | Integracja runtime | po D3; F3.1 T3 już na `main`; ominąć capture dla `timeout`/`cancelled` (po B7) |
| D5 (T5) | CLI `--record-diffs` + serve | po D4 |
| D6 (T6) | Docs, zamknięcie, ADR-0026 -> accepted | po D5 |

Wymagania z #84: domyślnie wyłączone; sekrety nigdy w eventach (redakcja
treści przed diffem, nie po); brak hooków/filtrów Git; nic nie zapisujemy w
`.git`; błąd Git nie zmienia wyniku attempt; starsze streamy dekodują.
Panel Compare i przełącznik desktop = osobny follow-up.

## 8. Blok E - F2.3 edytor edge/data mapping (spec `F2.3-edge-data-mapping-editor.md`)

E5a.1 jest na `main`. Maciej dostarcza UI:

| Slice | Opis | Zależności / testy |
| --- | --- | --- |
| E5a.2 | Panel Bindings; najpierw wynieść stan selekcji z `GraphCanvas` do wspólnego właściciela | po E5a.1; reducer + komponenty, `npm run check`, stories, e2e |
| E5a.3 | Graph inputs/outputs + hinty | po E5a.2; tylko wymagane outputy jako graph outputs (decyzja 8) |
| E5b.1, E5b.2 | Wartości na krawędziach, inspektor krawędzi | po E5a.2; mogą wyprzedzić E5a.3 |

Uwaga: desktop ma też zmiany w PR #127 (Kacper) - rebase przed pracą w
`desktop/src/...` w tych samych plikach.

## 9. Blok F - F3.9 Execution Policy v0 (później)

Warunki startu: F3.8 zmergowane; F3.4 T1-T3 (proctree) na `main`; F3.1 CLI
(jest). T6-T7 (domena + schemat) mogą ruszyć wcześniej niezależnie. T10-T11
wchodzą wyłącznie przez `proctree.Start` z hookiem `Prepare`, bez nadpisywania
`SysProcAttr` ustawionego przez proctree (test `TestStartKeepsCallerSysProcAttr`).
Faza Windows F3.4 (T10-T13) po F3.9 i naprawie #87.

## 10. Rytm pracy i reguły (z `AGENTS.md`)

- Jedno zadanie = jedna gałąź = jeden PR; prefiksy `feat(scope):`, `test(...)`, `docs(...)`.
- Przed kodem: przeczytać spec, plan, ADR; zgłosić plan (stan, zmiana, pliki, testy, kontrakty, ryzyka, niejasności).
- `TASKS.md` w katalogu głównym worktree dla pracy >3 kroków.
- Zawsze `make check` (+ `npm run check` w `desktop/` dla UI); raportować realny wynik.
- Testy bez `sleep` (bariery/kanały/fake clock), `go test -race` zielone.
- Nowy kontrakt trwały/eventowy -> ADR; zmiana `proposed` -> `accepted` w PR domykającym funkcję.
- Review: drugi agent/osoba (Kacper) - implementer i reviewer rozdzieleni.

## 11. Ryzyka

| Ryzyko | Mitygacja |
| --- | --- |
| Kolizje schema (12/13/14) i literały w testach | grep literałów; re-read `SchemaVersion` przed merge |
| Konflikt z PR #124-#128 (store/serve/desktop) | nie dotykać tych plików; rebase po merge |
| Decyzje "leadera w imieniu product ownera" (F3.4, F3.8/9, F2.3) | krótkie potwierdzenie z Kacprem przed T7/T3 |
| macOS (#24) bez maszyny | oznaczyć jako zablokowane |
| Windows testy (#87) | warunek T12, nie ruszać wcześniej |
| ADR numbering po PR #80 (REL1/REL2) | na `main` już istnieją 0030/0031; sprawdzić `docs/adr/` przed nowym numerem |

## 12. Otwarte pytania do Kacpra

1. Odpowiedzi na pytania 1-10 z planu F2.8b (szczególnie 3-9).
2. Potwierdzenie sześciu decyzji F3.4 przed B7.
3. Czy zaczynać F3.8 równolegle z F3.4, czy sekwencyjnie.
4. Terminy: czy F3.3 Resume ma dostać spec zaraz po domknięciu F3.2 (#128).
