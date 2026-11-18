# Przegląd specyfikacji i roadmapy względem repozytorium — 2026-11-18

> Migawka stanu `main` w commicie `4578233`. Wyniki szczegółowe:
> `docs/progress/roadmap-features-status.md` (wszystkie 65 feature'ów z PDF)
> i `docs/progress/architecture-plan-status.md` (sekcje planu architektury).
> Poprzedni audyt: `2026-10-19-repository-and-roadmap.md`.

## Zakres i metoda

Źródła porównania (dostarczone przez właściciela):

- „Agent Execution Control Plane — Roadmap produktu i szczegółowy katalog
  feature'ów” (36 stron, fazy 0–6, 65 feature'ów);
- „Agent Execution Control Plane — Architektura produktu, plan MVP i
  wykorzystanie w pracy licencjackiej” (27 stron, wersja 0.1).

Dla każdego feature'u porównano zakres i kryteria akceptacji z PDF z: specem w
`docs/features/`, ADR-ami, kodem (`cmd/`, `internal/`, `desktop/`) i testami.
Statusy deklarowane w dokumentacji repo zweryfikowano w kodzie tam, gdzie było to
możliwe (typy eventów, rodzaje Gate'ów, stany, trasy API, ekrany desktopu, pola
store'u, flagi CLI). Plik grafu w formie JSON sprawdzono ręcznie
(`graph validate` → valid).

Wykonane bramki:

| Polecenie | Wynik |
| --- | --- |
| `make check` | **OK** — wszystkie pakiety Go |
| `cd desktop && npm ci && npm run check` | **OK** — 32 pliki testów, 333 testy, typecheck, build, Storybook, skan bundla |
| `make desktop-e2e`, `make desktop-stories` | **Nie uruchamiane** w tym przeglądzie |

## Wynik w skrócie

| Faza | ✅ | 🟡 | 📝 | ⬜ | Exit criteria |
| --- | ---: | ---: | ---: | ---: | --- |
| 0 Execution Core | 7 | 0 | 0 | 0 | spełnione |
| 1 Agent Loop | 8 | 0 | 0 | 0 | spełnione (demo Codex 2026-11-17) |
| 2 Local IDE | 4 | 8 | 0 | 0 | częściowo |
| 3 Local Production Ready | 1 | 4 | 3 | 3 | niespełnione |
| 4–6 Cloud, Team, Managed | 0 | 0 | 0 | 27 | nie rozpoczęte |
| **Razem (65)** | **20** | **12** | **3** | **30** | |

✅ zrobione · 🟡 częściowo · 📝 tylko spec/ADR/plan · ⬜ brak specu i kodu.

## Co jest zrobione

- **Silnik (fazy 0–1) kompletny względem PDF**: schemat i walidator grafu,
  bindingi, scheduler z równoległością, Shell, eventy (schema v11), CLI;
  kontrakt adaptera, adapter Codex, streaming, Gate (exit code, predykat JSON,
  scope zgłoszony i obserwowany z Gita), Check Result, pętla korekcyjna z
  limitem, return envelope ze scoped retry, scope check warn/deny.
- **Ponad roadmapę**: pętla dla Shell node (F1.6a), node `human` z CLI i UI
  (F2.10), opcjonalny zapis wartości runa (F2.3-E6), izolacja worktree (F3.1).
- **UI (desktop Electron + React)**, ekrany: Dashboard, Runs (historia), Run
  (mapa live, KPI, timeline, log eventów, panel node'a, porównanie attemptów),
  Approvals, New run, Editor (canvas React Flow, YAML w CodeMirror, inspektor
  node'a, panel Issues, picker szablonów), Welcome (capability), Settings.
  PL/EN, design system, Storybook dev-only, e2e Electron w CI.

## Czego brakuje

### UI (faza 2)

1. **Edytor bindingów** (F2.3 E5a.2, E5a.3) — brak panelu listy bindingów,
   edycji graph inputs/outputs i podpowiedzi równoległości.
2. **Inspektor edge'a z wartością** (F2.3 E5b.1, E5b.2).
3. **Run Inspector — Overview** (F2.6): attempty na timeline, klikalne wiersze,
   czas agenta osobno, czas oczekiwania, critical path, filtr slow, ID/start/
   wersja runa.
4. **Run Inspector — Node Detail** (F2.7): wartości wejścia/wyjścia, aktywność
   agenta (znormalizowane eventy), changed files poza porównaniem, adapter i
   wersja, czasy, podsumowanie wejścia próby, znaczniki obcięcia logu.
5. **Diff między attemptami** (F2.8b; ADR-0026 proposed, plan gotowy).
6. **Human Gate panel**: blast radius, changed files, link do diffu; reject do
   ścieżki alternatywnej.
7. **Historia**: filtry per repo/graph, branch/commit, retencja (`runs prune`).
8. **Cancel runa z UI** — API nie ma trasy anulowania (F3.4).
9. **Sekcja permissions** w inspektorze (zależna od F3.9).
10. **Onboarding**: Docker, lista repo, self-check; weryfikacja macOS/Windows.
11. **Szablony** z PDF: fix issue, code review, dependency update, repo audit.
12. Ekrany fazy 4–5 (Runners, Projects, Analytics, Admin) — tylko wyszarzone
    pozycje menu.

### Silnik (faza 3)

- F3.2 Durable Run State — zadania 5–9 z 9 (crash testy, snapshot, retencja,
  API/desktop).
- F3.3 Resume/Recovery — brak specu.
- F3.4 Cancellation/Timeouts — spec, ADR, plan; brak kodu (timeouty per
  node/graph, drzewo procesów Shell, reason timeoutu, Windows).
- F3.5 Artifact Store — brak specu.
- F3.6 Graph Versioning — hash i snapshot są; brak commita/brancha, wersji
  runtime, wersji promptu; brak specu.
- F3.7 Export JSON/CSV — brak specu (blokuje część eksperymentalną pracy).
- F3.8 Secrets, F3.9 Execution Policy — spec, ADR, plan; brak kodu.
- F3.10 Usage — kontrakt jest, Codex nie raportuje danych.
- F3.11 Diagnostics — brak bundla i self-testu.

## Świadome odstępstwa od PDF (udokumentowane)

| PDF | Repo | Decyzja |
| --- | --- | --- |
| SQLite dla historii (F2.9, arch. 13.2) | JSONL per run + `store.json` | ADR-0019, ADR-0027 (proposed) |
| Tauri rekomendowane (16.2) | Electron | F2.5 / ADR-0019 |
| Monaco (16.1) | CodeMirror 6 | F2.1 |
| WebSocket dla live events | SSE | ADR-0019 |
| `agentctl` | `agentplane` (planowane `tarmind`, REL1) | REL1 draft |
| `depends_on`, `foreach`, `when` w grafie | jawne `edges` i `graph_input_bindings`, statyczne unity | ADR-0001, ADR-0003, ADR-0015 |
| Stany node'a CHECKING/CORRECTING/WAITING_FOR_HUMAN | fazy attemptu i eventy approval; UI wyprowadza `retrying`/`human_waiting` | ADR-0014, ADR-0017 |
| Human gate wg blast radius | jawny node `human` | F2.10 non-goals |
| Resolved prompt/command w node detail | celowo nie zapisywane | ADR-0011, F2.7 decyzja 9 |

## Ustalenia według wagi

### Wysokie

**H1 — Faza 3 jest warunkiem Demo D, v1.0 i części eksperymentalnej, a jej
kluczowe pozycje nie mają specu.** F3.3 Resume, F3.5 Artifact Store i F3.7
Export nie mają specyfikacji; F3.6 ma tylko fragmentaryczną implementację bez
specu. Bez F3.7 nie ma stabilnego wejścia do analizy RQ1–RQ5, a
`experiments/run-log.csv` jest pusty. **Działanie:** po F3.2/F3.4 napisać specy
F3.3 i F3.7 jako następne.

**H2 — brak anulowania i timeoutów w ścieżce produktowej.** Run można przerwać
tylko sygnałem procesu CLI; desktop nie ma przycisku Cancel, API nie ma trasy,
Shell ubija tylko proces bezpośredni, nie ma timeoutów. Długie runy z agentem
nie mają twardego limitu czasu. **Działanie:** F3.4 według gotowego planu.

### Średnie

**M1 — Run Inspector nie spełnia jeszcze roli „debuggera zamiast terminali”**
(Demo C, v1.0 punkt 3): brak wartości wejścia/wyjścia, aktywności agenta i
diffu. Specy F2.6/F2.7 są zaakceptowane i nie wymagają zmian silnika.

**M2 — „niezaakceptowany attempt nie przecieka” jest gwarantowane dla plików
tylko z `isolation: worktree` i tylko dla Agent node.** Shell nadal pisze we
wspólny workspace. To jest zgodne z ADR-0024, ale warto to pokazywać w UI
(np. ostrzeżenie przy node'ach bez izolacji).

**M3 — siedem ADR-ów (0025–0031) ma status `proposed`, a na dwóch z nich
stoi już kod na `main`**: ADR-0025 (capability report — zaimplementowany), ADR-0027 (durable
state — zadania 1–4 na `main`). Wymaga decyzji właściciela: zaakceptować albo
oznaczyć, które części są jeszcze otwarte.

### Niskie / porządkowe

**L1 — nieaktualne statusy w dokumentacji (poprawione w tym przeglądzie):**
`README.md` i `docs/roadmap.md` mówiły, że demo F1.7 z prawdziwym Codexem nie
zostało wykonane (wykonane 2026-11-17); `docs/features/README.md` mówił
„desktop pending” dla F2.11 i „desktop picker pending” dla F2.12 (oba
zaimplementowane, #97 i #98); status w specu F1.7 mówił o oczekującym demo.

**L2 — `docs/roadmap.md` nie miał kolumny statusu dla faz 2–3**, więc nie było
jednego miejsca z odpowiedzią „co jest zrobione”. Dodano kolumnę statusu i
odnośnik do dokumentów postępu.

**L3 — F1.5 ma w specu „pending independent review”.** Przegląd nie znalazł
zapisu takiego review; pozostawiono bez zmian do decyzji właściciela.

**L4 — zestaw szablonów (F2.12) różni się od PDF.** To decyzja produktowa, nie
błąd; w roadmapie powinno być jasne, czy szablony z PDF są nadal planowane.

## Zmiany wprowadzone razem z tym raportem

- dodano `docs/progress/roadmap-features-status.md` i
  `docs/progress/architecture-plan-status.md`;
- zaktualizowano statusy w `README.md`, `docs/roadmap.md`,
  `docs/features/README.md`, `docs/features/F1.7-return-envelope-scoped-retry.md`
  oraz odnośniki w `docs/reference/README.md`.

Kod nie był zmieniany.

## Granice przeglądu

Przegląd nie obejmował ręcznego audytu bezpieczeństwa każdej ścieżki kodu ani
uruchomienia e2e desktopu i prawdziwego Codexa; wyniki tych bramek przywołano z
`docs/validation/`. Statusy „✅” oznaczają pokrycie kryteriów akceptacji z PDF z
zawężeniami opisanymi w specach, a nie pełny zakres opisów „Zakres funkcjonalny”.
