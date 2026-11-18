// Package diff renders bounded unified line diffs. It is pure: no I/O, no
// clock, standard library only. Callers decide what is binary or too large to
// diff; this package only bounds CPU (edit limit, context) and output size.
package diff
