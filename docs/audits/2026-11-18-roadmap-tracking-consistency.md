# Śledzenie roadmapy w repo - spójność i propozycja jednego ledgera (2026-11-18)

Stan: `main`/gałąź robocza na `fdaae01`. Porównano repo z trzema dokumentami
przeglądu (`Przeglad_spec_vs_repo`, `Architektura_i_plan - stan realizacji`,
`Roadmapa - stan realizacji`), które opisują stan z commitu `4578233`.

## 1. Najważniejsze ustalenia

1. **Dokumenty przeglądu odwołują się do plików, których nie ma w repo.**
   Wszystkie trzy wskazują `docs/progress/roadmap-features-status.md` i
   `docs/progress/architecture-plan-status.md` oraz
   `docs/audits/2026-11-18-spec-vs-repository.md`. Żaden z nich nie istnieje na
   `main` ani w historii; raport mówi też o poprawkach w `README.md`,
   `docs/roadmap.md`, `docs/features/README.md`, `F1.7` i `docs/reference/README.md`,
   a w repo te pliki nadal zawierają stare stwierdzenia (np. "demo F1.7 nie
   wykonane"). Przegląd powstał w osobnym drzewie roboczym i nie został scalony.
2. **Przegląd jest już nieaktualny o 16 commitów** (`4578233..fdaae01`, #124-#143).
   Od jego migawki scalono: F3.2 zadania 5-9 (przegląd: "do zrobienia"), F3.4
   zadania 1-6 (przegląd: "spec, brak kodu"), F2.8b T1-T2 (przegląd: "brak kodu").
3. **Brak jednego źródła prawdy.** Status feature'a jest dziś zapisany w co
   najmniej 10 miejscach, które nie są ze sobą synchronizowane (tabela poniżej).
4. **"Zrobione" znaczy dwie różne rzeczy.** Spec/ADR mówi "implemented", gdy
   spełnione są jego kryteria akceptacji. Przegląd mówi 🟡, gdy zakres z oryginalnego
   PDF jest szerszy niż spec (SQLite vs JSONL, zestaw szablonów, brak filtrów).
   To nie sprzeczność faktów, tylko dwie osie zapisane w jednej kolumnie.
   Stąd F2.9, F2.10, F2.11, F2.12, F3.2: "implemented" w specu, 🟡 w przeglądzie.

## 2. Gdzie status jest dziś zapisany i co jest nieaktualne

| Miejsce | Rola | Problem |
| --- | --- | --- |
| `AGENTS.md` "Current product phase" | stan dla agentów | pisze o event schema 11; kod ma `SchemaVersion = 13`; nie wspomina F3.4 ani F2.8b |
| `README.md` "Status" | stan dla ludzi | "demo F1.7 prepared but not yet executed" (wykonane 2026-11-17); "event schema 10" w l. 119 |
| `docs/roadmap.md` | indeks inżynierski | nagłówek "Phase 1 (real-agent demo pending)"; tabele faz 2-3 bez kolumny statusu (przegląd L2); wpis F2.9 nadal mówi "SQLite" |
| `docs/features/README.md` | tabela specyfikacji | F3.4 "Draft", F2.8 bez informacji o T1-T2, "schema 11"; brak F3.3/3.5/3.6/3.7/3.10/3.11 (słusznie, bez specu, ale nigdzie nie oznaczone jako luka) |
| nagłówki `**Status:**` w 40 specach | kontrakt feature'a | wolny tekst (np. F2.3 ma 5 linii); F3.4 i F3.4a "no implementation yet" mimo scalonych zadań 1-6; F1.5 "pending independent review", której przegląd nie znalazł (L3) |
| `docs/reference/implementation-queue.md` | archiwum | "execution evidence is pending" dla F1.7 |
| `docs/superpowers/plans/*.md` | plany zadań | checkboxy nie są odhaczane: plan F3.2 ma 0/39 mimo zamkniętego F3.2, plan F2.8b 0/18 mimo T1-T2, plan F3.4 7/30 przy 6 scalonych zadaniach |
| `docs/adr/*.md` | decyzje | ADR-0027 `proposed`, mimo że F3.2 jest wdrożony; ADR-0025 zaimplementowany (przegląd M3) |
| `docs/architecture.md` | architektura | równocześnie schema 11, 12 i 13 w różnych sekcjach |
| `docs/features/REL2` + ADR-0031 | publiczny feed | już zakłada, że źródłem są nagłówki specyfikacji; landing ma "14 of 15 done" |
| `docs/reference/full-*.md` | oryginał z PDF | poprawnie niezmienny, ale nie oznaczony jako "nie status" |

## 3. Różnice między przeglądem a repo na `fdaae01`

| Feature | Przegląd (`4578233`) | Repo teraz | Uwaga |
| --- | --- | --- | --- |
| F3.2 | 🟡 zadania 5-9 otwarte | wdrożone na Linux (specyfikacja: tasks 1-9) | Windows nie uruchomiony; ADR-0027 nadal `proposed` |
| F3.4 | 📝 spec, brak kodu | w toku, zadania 1-6 z 11 scalone (proctree, kill drzewa Shell/Codex/Gate, typy przyczyn, `timeout_seconds`) | najwyższa pozycja kolejki; H2 z przeglądu częściowo nieaktualne |
| F2.8 | 🟡 8b "brak kodu" | pakiet line diff i event `attempt_diff` scalone | kod 8b jest, UI i capture nie |
| event schema | 11 | **13** | AGENTS.md, README.md, features/README.md |
| H1 (F3.3, F3.5, F3.7 bez specu) | otwarte | nadal otwarte | najpoważniejsza luka planistyczna |

Pozostałe wnioski przeglądu (H1, M1-M3, L1-L4) wciąż obowiązują; L1 i L2 są
"poprawione" tylko w niescalonym drzewie.

## 4. Propozycja: jeden ledger

`docs/roadmap-ledger.yaml` (dodany razem z tym raportem) jest jedynym miejscem,
w którym status feature'a jest zapisany ręcznie. Zawiera: 65 pozycji z katalogu
PDF (F0.1-F6.5), pozycje dodatkowe (F1.6a, F2.3-E6, F2.5a, REL1, REL2), stan faz,
demo A-E, a dla każdej pozycji: `status`, `coverage`, linki do specu/ADR, listę luk,
`verified` (commit) i `source` (`code` albo `review@4578233`).

Rozdzielone osie:

- `status`: `planned` (brak specu) / `draft` / `accepted` / `in_progress` /
  `implemented`. To stan kontraktu repo (spec + ADR + kod). Słownik jest zgodny z
  zamkniętym słownikiem REL2 rozszerzonym o `planned` i rozdzielonym `accepted`.
- `coverage`: `full` / `partial` / `none`. Ile z oryginalnego zakresu PDF dostarcza
  `implemented`. Niepełne pokrycie wymaga wpisu w `gaps`.

Wynik na `fdaae01` (65 pozycji katalogu): 25 implemented, 5 in_progress, 2 draft,
33 planned. Przegląd liczył 20/12/3/30 w jednej kolumnie. Różnica to F2.9-F2.12 i
F3.2 (przeniesione na `implemented` + `coverage: partial`), F3.4 (`in_progress`)
oraz to, że `draft` (F3.8, F3.9) i `planned` bez specu są teraz rozdzielone od
`in_progress`.

Pozycje `source: review@4578233` nie były ponownie sprawdzone w kodzie; wartość
`verified: fdaae01` oznacza tylko, że żaden z 16 nowszych commitów ich nie dotyka
(sprawdzone przez `git log`). Ledger wprost to zaznacza, zamiast udawać świeży audyt.

### Co stałoby się źródłem, a co widokiem

| Dokument | Po wdrożeniu ledgera |
| --- | --- |
| `docs/roadmap-ledger.yaml` | **źródło**: status, coverage, luki, ID, priorytety, zależności |
| `docs/roadmap.md` | wygenerowany widok (tabele faz ze statusem), proza celów faz zostaje |
| `docs/features/README.md` | wygenerowana tabela specyfikacji |
| nagłówek `**Status:**` w specu | pierwsze słowo musi równać się `status` z ledgera (sprawdzane); reszta to opis |
| `README.md`, `AGENTS.md` | jedno zdanie + link do ledgera; liczba schematu zdarzeń tylko z kodu |
| checkboxy planów | postęp zadań planu zostaje w planie, ledger ma tylko status feature'a |
| `docs/roadmap.json` (REL2) | generowany z ledgera zamiast ze specyfikacji; wymaga poprawki REL2/ADR-0031 |
| PDF-y `docs/reference/full-*` | zostają jako oryginał, oznaczone "nie jest źródłem statusu" |

### Wymuszanie (bez niego dryf wróci)

1. Test/skrypt w `make check`: słownik zamknięty; unikalne ID; każde ID z
   `roadmap.md` jest w ledgerze; `spec` wskazuje istniejący plik; pierwszy token
   `**Status:**` w specu zgadza się z ledgerem; `coverage != full` wymaga
   niepustego `gaps`; `implemented` wymaga specu; `verified` jest poprawnym commitem.
2. Generator widoków (`make roadmap`) i test na nieświeży wynik, jak w REL2.
3. Reguła w AGENTS.md "Definition of done": PR zmieniający status feature'a
   zmienia ledger w tym samym PR.

Zgodnie z AGENTS.md (zmiana trwałego kontraktu, poprawka REL2/ADR-0031) punkty 1-3
wymagają krótkiego specu i decyzji właściciela; nie są częścią tego commita.

## 5. Decyzje do podjęcia przez właściciela

1. Czy rozdzielenie `status` / `coverage` jest właściwe? (Alternatywa: jedna oś
   z 🟡, co odtwarza obecny problem.)
2. Czy ledger zastępuje źródło statusu z REL2 (nagłówki specyfikacji)? Rekomendacja: tak.
3. Format: YAML (rekomendowany; biblioteka YAML już jest w zależnościach) czy JSON.
4. Czy zaakceptować ADR-0027 (F3.2 wdrożony) oraz ADR-0025 (przegląd M3)?
5. Czy `docs/progress/*` z przeglądu mają zostać odtworzone, czy ledger je zastępuje
   (rekomendacja: zastępuje; "Architektura - plan" potrzebuje osobnego widoku
   zapisanego jako sekcja ledgera lub pliku powiązanego).
