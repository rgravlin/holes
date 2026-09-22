package holes_test

import (
	"fmt"

	"github.com/rgravlin/holes"
)

func ExampleFind() {
	d := holes.Date{}
	var days []int64
	for _, s := range []string{"2026-09-01", "2026-09-02", "2026-09-05", "2026-09-06"} {
		p, err := d.Parse(s)
		if err != nil {
			fmt.Println(err)
			return
		}
		days = append(days, p)
	}
	to, err := d.Parse("2026-09-08")
	if err != nil {
		fmt.Println(err)
		return
	}

	gaps, err := holes.Find(days, holes.Options{To: &to})
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, g := range gaps {
		fmt.Printf("%s..%s (%d missing)\n", d.Format(g.First), d.Format(g.Last), g.Count)
	}
	// Output:
	// 2026-09-03..2026-09-04 (2 missing)
	// 2026-09-07..2026-09-08 (2 missing)
}
