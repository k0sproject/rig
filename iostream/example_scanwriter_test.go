package iostream_test

import (
	"fmt"
	"io"
	"strings"

	"github.com/k0sproject/rig/v2/iostream"
)

func ExampleScanWriter() {
	contentReader := strings.NewReader("Hello, World!\nHow are you today?\nNice to meet you.\n")
	i := 1
	sw := iostream.NewScanWriter(func(row string) {
		fmt.Println(i, row)
		i++
	})
	defer sw.Close()

	_, _ = io.Copy(sw, contentReader)
	// Output:
	// 1 Hello, World!
	// 2 How are you today?
	// 3 Nice to meet you.
}
