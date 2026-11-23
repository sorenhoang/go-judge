# Phase 7–11: I/O judge + AI problem generator

## Goal

Paste text, or upload an image or PDF of a problem statement. The system then:

1. extracts the spec: constraints, subtasks, I/O format, and whether many answers can be valid;
2. rewrites the statement in Vietnamese and English;
3. writes the main solution, a brute-force solution, the generator, the validator and (if needed) the checker;
4. **proves its own output in the sandbox** before a human publishes it.

AI output is untrusted. Every AI artifact goes through the same verification pipeline as one
uploaded by a human. The AI is only one producer of a problem package.

## Decisions

| Topic | Decision | Why |
|---|---|---|
| Judge model | stdin/stdout only, drop `go test` problems | generator/validator/checker only make sense for I/O problems |
| Tool language | C++17 + `testlib.h` | Codeforces/Polygon/VNOI standard; `readInt(l, r)`, `ensuref`, checker API built in; the AI knows it well |
| Submission languages | C++17, Python 3 (more later) | a `languages` table holds compile/run commands, so a new language is a new row, not new code |
| Statement languages | `vi`, `en` | stored per language; the spec (constraints) is shared |
| Test storage | files on a shared volume `data/problems/{id}/tests/{n}.in/.ans` | tests can be MBs; Postgres stores metadata only. ponytail: local volume, move to S3/MinIO when there is more than one host |
| AI provider | Claude API (`claude-opus-5-5`), vision + PDF input, structured output via tool use | no separate OCR step |

## Verification pipeline (Phase 8, the core)

```
validator.cpp ─┐
generator.cpp ─┼─► compile all (CE = fail)
checker.cpp   ─┤
main.cpp      ─┤
brute.cpp     ─┘
      │
      ▼
gen script (e.g. "gen 1 10 > t1") ─► each test ─► validator (must accept, + subtask group check)
      │
      ▼
main.cpp produces .ans for each test (TLE vs time limit = fail)
      │
      ▼
stress: K random small tests ─► main vs brute
        exact-output problems: diff
        multi-answer problems: checker(input, output, answer_of_other) must accept both ways
      │
      ▼
package VERIFIED  /  FAILED + log of the first failing step
```

## Phases

### Phase 7: I/O judge
- Migration: drop `starter_code`/`test_code`; add `time_limit_ms`, `memory_limit_mb`, `checker_type` (`exact` | `tokens` | `testlib`); new tables `problem_statements(problem_id, lang, title, legend, input_spec, output_spec, notes)`, `subtasks(problem_id, idx, points, constraints_text)`, `tests(problem_id, idx, subtask_idx, is_sample)`, `languages(id, compile_cmd, run_cmd, image)`.
- `submissions` gets `language`, `score`, and per-test results (`submission_tests`: verdict, time_ms, memory_kb).
- Runner: compile once, then run each test with its own time/memory limit. Verdicts: `AC WA TLE MLE RE CE`. Subtask score = points if every test in it is AC.
- Sandbox image: `gcc` + `python3` + `testlib.h`.
- Seed: rewrite the 3 old problems as I/O problems, one of them multi-answer (checks the checker path).

### Phase 8: Problem package + verification (no AI yet)
- `problem_packages` table: source of solution/brute/generator/validator/checker, gen script, status `DRAFT → BUILDING → VERIFIED | FAILED`, `build_log`.
- `POST /problems/{id}/package/build` queues a build job (RabbitMQ, the same worker pattern as Phase 3–4) that runs the pipeline above.
- Unit tests use hand-written known-bad packages: a validator that lets a bad test through, a main solution with an off-by-one, a checker that rejects a valid answer. Each one must make the build FAIL at the right step.

### Phase 9: AI ingest (image / PDF / text → spec)
- `POST /drafts` (multipart: file or text) → Claude with a JSON schema tool `extract_problem_spec`: limits, I/O format, subtasks with their own constraints, `multiple_answers: bool`, samples.
- The draft is stored and can be edited in the UI before generation (the AI can misread a constraint in a photo).
- Tests replay recorded Claude responses (fixtures); no API key needed in CI.

### Phase 10: AI author + repair loop
- From the approved spec, Claude writes the VI and EN statements and the 5 sources + gen script, then Phase 8 build runs.
- On FAILED: send the failing step + log back to Claude, regenerate only the broken artifact, at most `MAX_REPAIR_ROUNDS` (config) times.
- Progress streams over SSE (the same endpoint pattern as Phase 6): `extracting → writing → building → stress 340/500 → verified`.

### Phase 11: Review + publish
- A reviewer reads the statement (VI/EN tabs), samples, build log and stress result, then publishes. Nothing the AI generates is published automatically.

## Out of scope for now
Interactive problems, output-only problems, more than 2 submission languages, and multi-host test storage.
