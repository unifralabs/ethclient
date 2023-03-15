package ethclient

import (
	"log"
	"net/http"
	_ "net/http/pprof"
)

func StartPprof() {
	go func() {
		log.Println("pprof listen on 6060")
		log.Println(http.ListenAndServe(":6060", nil))
	}()
}
