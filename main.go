package main

import "flag"
import "log"
import "net/http"
import "runtime"

var listenAddr = flag.String("listen", ":8118", "The [IP]:port to listen for incoming connections on.")
var workers = flag.Int("workers", runtime.NumCPU(), "The number of worker threads to execute.")

func main() {
	flag.Parse()

	setRlimitFromFlags()

	runtime.GOMAXPROCS(*workers)
	for i := *workers - 1; i >= 0; i-- {
		go tarpitTimer()
	}

	http.HandleFunc("/", tarpitHandler)
	http.HandleFunc("/robots.txt", robotsDisallowHandler)
	log.Fatal(http.ListenAndServe(*listenAddr, nil))
}
