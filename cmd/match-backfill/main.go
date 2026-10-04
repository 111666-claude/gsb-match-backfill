// Command match-backfill 跑补人队列样例。
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"example.com/backfill"
)

// Run 执行一次命令行调用，返回退出码。
func Run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("match-backfill", flag.ContinueOnError)
	flags.SetOutput(stderr)
	sample := flags.String("sample", "rating", "rating / need / ttl / dup / scan")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	switch *sample {
	case "rating":
		pool := backfill.New(100000)
		pool.Join("a", 3000, 0)
		fmt.Fprintf(stdout, "picked=%d\n", len(pool.Fill(1, 1000, 2000, 0)))
	case "need":
		pool := backfill.New(100000)
		for index := 0; index < 5; index++ {
			pool.Join(fmt.Sprintf("p-%d", index), 1500, index)
		}
		fmt.Fprintf(stdout, "picked=%d\n", len(pool.Fill(2, 1000, 2000, 10)))
	case "ttl":
		pool := backfill.New(5000)
		pool.Join("a", 1500, 0)
		fmt.Fprintf(stdout, "picked=%d\n", len(pool.Fill(1, 1000, 2000, 10000)))
	case "dup":
		pool := backfill.New(100000)
		pool.Join("a", 1500, 0)
		second := pool.Join("a", 1500, 10)
		fmt.Fprintf(stdout, "second=%v pending=%d\n", second, pool.Pending(10))
	case "scan":
		pool := backfill.New(100000)
		for index := 0; index < 3000; index++ {
			pool.Join(fmt.Sprintf("p-%d", index), 1500, 0)
		}
		for index := 0; index < 3000; index++ {
			pool.Pending(0)
		}
		fmt.Fprintf(stdout, "scanned=%d\n", pool.Scanned())
	default:
		fmt.Fprintln(stderr, "需要 --sample rating|need|ttl|dup|scan")
		return 2
	}
	return 0
}

func main() {
	os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr))
}
