# shellescape

A drop-in replacement for [alessio/shellescape](https://github.com/alessio/shellescape) / ["gopkg.in/alessio/shellescape.v1"]("gopkg.in/alessio/shellescape.v1").

It's a tiny bit faster and allocates a tiny bit less. It's quite unlikely that anyone will notice any difference in a real-world application, the is here just to reduce dependencies of the `rig` package.

To use, replace `alessio/shellescape` with `github.com/k0sproject/rig/v2/sh/shellescape` in your imports.

In addition to the original package, this package also includes `Unquote`, `Split` and `Expand` (which supports `${var}`, `$var`, `$(cmd)` and `${var:-word}` and some other expansions).

## Benchmarks

Just out of curiosity.

### Quote

```text
BenchmarkQuote/#00-12                       382379658   3.037 ns/op     0 B/op    0 allocs/op
BenchmarkQuote/"double_quoted"-12           14906526    79.10 ns/op    24 B/op    1 allocs/op
BenchmarkQuote/with_spaces-12               16851456    71.15 ns/op    16 B/op    1 allocs/op
BenchmarkQuote/'single_quoted'-12           8453859     154.4 ns/op    72 B/op    2 allocs/op
BenchmarkQuote/;-12                         25321892    45.73 ns/op     3 B/op    1 allocs/op
BenchmarkQuote/;${}-12                      22446418    54.00 ns/op     8 B/op    1 allocs/op
BenchmarkQuote/foo.example.com-12           39565838    29.85 ns/op     0 B/op    0 allocs/op

BenchmarkQuoteAlessio/#00-12                678336361   1.773 ns/op     0 B/op    0 allocs/op
BenchmarkQuoteAlessio/"double_quoted"-12    10867801    129.4 ns/op    24 B/op    1 allocs/op
BenchmarkQuoteAlessio/with_spaces-12        6283456     187.8 ns/op    16 B/op    1 allocs/op
BenchmarkQuoteAlessio/'single_quoted'-12    6507007     180.3 ns/op    56 B/op    2 allocs/op
BenchmarkQuoteAlessio/;-12                  11453983    163.8 ns/op     3 B/op    1 allocs/op
BenchmarkQuoteAlessio/;${}-12               10179727    123.2 ns/op     8 B/op    1 allocs/op
BenchmarkQuoteAlessio/foo.example.com-12    3416793     357.8 ns/op     0 B/op    0 allocs/op

```

### QuoteCommand

```text
BenchmarkQuoteCommand/Basic_Command-12            7548391       159.4 ns/op      88 B/op       2 allocs/op
BenchmarkQuoteCommandAlessio/Basic_Command-12     2256212       572.1 ns/op      96 B/op       3 allocs/op
```

### StripUnsafe

Looks like there isn't much difference between the two.

```text
BenchmarkStripUnsafe/Hello,_World!-12                        33246037    34.29 ns/op     0 B/op    0 allocs/op
BenchmarkStripUnsafe/\x00\x01\x02Test\x03\x04\x05-12         16221404    71.40 ns/op    16 B/op    1 allocs/op
BenchmarkStripUnsafe/SpecialChars\x1f\x7f-12                 10196348    113.1 ns/op    24 B/op    1 allocs/op
BenchmarkStripUnsafe/中文测试-12                             14912823    79.82 ns/op     0 B/op    0 allocs/op
BenchmarkStripUnsafe/#00-12                                  673622737   1.777 ns/op     0 B/op    0 allocs/op

BenchmarkStripUnsafeAlessio/Hello,_World!-12                 24444872    48.44 ns/op     0 B/op    0 allocs/op
BenchmarkStripUnsafeAlessio/\x00\x01\x02Test\x03\x04\x05-12  17340309    68.57 ns/op    16 B/op    1 allocs/op
BenchmarkStripUnsafeAlessio/SpecialChars\x1f\x7f-12          13657556    80.99 ns/op    24 B/op    1 allocs/op
BenchmarkStripUnsafeAlessio/中文测试-12                      13895878    85.13 ns/op     0 B/op    0 allocs/op
BenchmarkStripUnsafeAlessio/#00-12                           473620983   2.509 ns/op     0 B/op    0 allocs/op
```
