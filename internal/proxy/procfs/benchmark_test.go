package procfs

import (
	"testing"
)

func BenchmarkParseCPUStatLine(b *testing.B) {
	line := []byte("cpu  12345 678 9012 345678 111 222 333 444 555 666")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := parseCPUStatLine(line)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseProcStat(b *testing.B) {
	data := []byte("1234 (bash) S 1 1234 1234 34816 1234 4194304 1234 0 0 0 100 200 0 0 20 0 1 0 12345 12345678 1234 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := parseProcStat(data, 1234)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseProcStatus(b *testing.B) {
	status := []byte("Name:   python3\nVmSize:  25769803776 kB\nVmRSS:  12884901888 kB\n")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _, err := parseProcStatus(status)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseStatusValue(b *testing.B) {
	line := []byte("MemAvailable:   25678900 kB")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := parseStatusValue(line)
		if err != nil {
			b.Fatal(err)
		}
	}
}
