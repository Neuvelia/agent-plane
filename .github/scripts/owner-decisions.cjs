// Reads the "Owner decisions" section of a needs-owner issue
// (docs/roadmap/issue-triage.md) and says what the ticked options lead to.
// Pure functions only; the GitHub calls live in issue-labels.cjs.

// "##" in comments and agent-written bodies, "###" where an issue form renders
// the "Owner decisions" field. The section ends at the next heading.
const SECTION = /^#{2,3}\s+Owner decisions\s*$/m;
const NEXT_SECTION = /^#{1,3}\s/m;
const DECISION = /^\*\*(D\d+)\./;
const OPTION = /^\s*[-*]\s+\[( |x|X)\]\s+(?:([A-Z])[:.)]\s*)?.*?(?:`→\s*([A-Za-z]+)`)?\s*$/;
const OUTCOMES = new Set(['ready', 'adr', 'spec', 'split', 'blocked', 'close']);

function section(text) {
  const start = SECTION.exec(text || '');
  if (!start) return null;
  const rest = text.slice(start.index + start[0].length);
  const end = NEXT_SECTION.exec(rest);
  return end ? rest.slice(0, end.index) : rest;
}

// parse returns the decisions in order: [{id, options: [{key, checked, outcome}]}],
// or null when the text has no Owner decisions section. An option without a
// known marker has outcome "unknown", which never counts as ready.
function parse(text) {
  const body = section(text);
  if (body === null) return null;
  const decisions = [];
  for (const line of body.split('\n')) {
    const header = DECISION.exec(line.trim());
    if (header) {
      decisions.push({ id: header[1], options: [] });
      continue;
    }
    const option = OPTION.exec(line);
    if (!option || decisions.length === 0) continue;
    const outcome = (option[3] || '').toLowerCase();
    decisions[decisions.length - 1].options.push({
      key: option[2] || String(decisions[decisions.length - 1].options.length + 1),
      checked: option[1] !== ' ',
      outcome: OUTCOMES.has(outcome) ? outcome : 'unknown',
    });
  }
  return decisions;
}

// evaluate returns null without decisions, {complete: false} unless every
// decision has exactly one ticked option, and otherwise the chosen options and
// whether all of them lead to ready.
function evaluate(decisions) {
  if (!decisions || decisions.length === 0) return null;
  const chosen = [];
  for (const decision of decisions) {
    const ticked = decision.options.filter((option) => option.checked);
    if (ticked.length !== 1) return { complete: false };
    chosen.push({ id: decision.id, key: ticked[0].key, outcome: ticked[0].outcome });
  }
  return { complete: true, ready: chosen.every((item) => item.outcome === 'ready'), chosen };
}

// summary is a stable one-line key of the chosen options, used to post the
// follow-up comment only once per set of answers.
function summary(chosen) {
  return chosen.map((item) => `${item.id}=${item.key}:${item.outcome}`).join(';');
}

module.exports = { parse, evaluate, summary };
