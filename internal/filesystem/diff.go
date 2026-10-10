package filesystem

import "sort"

type editSpan struct{ a, b, c, d int }
type anchor struct{ a, b int }

// Unique shared lines anchor separate edits without a quadratic whole-file LCS.
// Regions containing only repeated lines remain a single conservative span.
func spans(old, next []string) []editSpan {
	out := []editSpan{}
	var visit func(int, int, int, int)
	visit = func(a, b, c, d int) {
		for a < b && c < d && old[a] == next[c] {
			a++
			c++
		}
		for a < b && c < d && old[b-1] == next[d-1] {
			b--
			d--
		}
		if a == b && c == d {
			return
		}
		type occurrence struct{ count, at int }
		left, right := map[string]occurrence{}, map[string]occurrence{}
		for i := a; i < b; i++ {
			v := left[old[i]]
			v.count++
			v.at = i
			left[old[i]] = v
		}
		for i := c; i < d; i++ {
			v := right[next[i]]
			v.count++
			v.at = i
			right[next[i]] = v
		}
		candidates := []anchor{}
		for i := a; i < b; i++ {
			l, r := left[old[i]], right[old[i]]
			if l.count == 1 && r.count == 1 {
				candidates = append(candidates, anchor{i, r.at})
			}
		}
		tails, predecessors := []int{}, make([]int, len(candidates))
		for i, p := range candidates {
			j := sort.Search(len(tails), func(j int) bool { return candidates[tails[j]].b >= p.b })
			predecessors[i] = -1
			if j > 0 {
				predecessors[i] = tails[j-1]
			}
			if j == len(tails) {
				tails = append(tails, i)
			} else {
				tails[j] = i
			}
		}
		if len(tails) == 0 {
			// Bound the repeated-line fallback to one million cells.
			if b > a && d > c && (b-a+1) <= 1<<20/(d-c+1) {
				width := d - c + 1
				table := make([]uint32, (b-a+1)*width)
				for i := b - a - 1; i >= 0; i-- {
					for j := d - c - 1; j >= 0; j-- {
						if old[a+i] == next[c+j] {
							table[i*width+j] = table[(i+1)*width+j+1] + 1
						} else {
							table[i*width+j] = max(table[(i+1)*width+j], table[i*width+j+1])
						}
					}
				}
				x, y := a, c
				for i, j := 0, 0; i < b-a && j < d-c; {
					if old[a+i] == next[c+j] {
						if x < a+i || y < c+j {
							out = append(out, editSpan{x, a + i, y, c + j})
						}
						i++
						j++
						x = a + i
						y = c + j
					} else if table[(i+1)*width+j] >= table[i*width+j+1] {
						i++
					} else {
						j++
					}
				}
				if x < b || y < d {
					out = append(out, editSpan{x, b, y, d})
				}
			} else {
				out = append(out, editSpan{a, b, c, d})
			}
			return
		}
		chain := []anchor{}
		for i := tails[len(tails)-1]; i >= 0; i = predecessors[i] {
			chain = append(chain, candidates[i])
		}
		for i := len(chain) - 1; i >= 0; i-- {
			p := chain[i]
			visit(a, p.a, c, p.b)
			a = p.a + 1
			c = p.b + 1
		}
		visit(a, b, c, d)
	}
	visit(0, len(old), 0, len(next))
	return out
}

func changedRanges(old, next []string, context int) ([]Range, []Range) {
	merged := []editSpan{}
	for _, s := range spans(old, next) {
		s.a = max(0, s.a-context)
		s.b = min(len(old), s.b+context)
		s.c = max(0, s.c-context)
		s.d = min(len(next), s.d+context)
		if len(merged) > 0 {
			p := &merged[len(merged)-1]
			if s.a <= p.b || s.c <= p.d {
				p.b = max(p.b, s.b)
				p.d = max(p.d, s.d)
				continue
			}
		}
		merged = append(merged, s)
	}
	before, after := []Range{}, []Range{}
	for _, s := range merged {
		if s.b > s.a {
			before = append(before, Range{Start: s.a + 1, Lines: append([]string{}, old[s.a:s.b]...)})
		}
		if s.d > s.c {
			after = append(after, Range{Start: s.c + 1, Lines: append([]string{}, next[s.c:s.d]...)})
		}
	}
	return before, after
}
