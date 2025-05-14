// This project is licensed under the GNU General Public License v2.0.
// See the LICENSE file for more details.
//
// This file is part of a project that utilizes functionalities implemented in rdma.go,
// which in turn contains code derived from rdma-core/libibverbs/examples/ud_pingpong.c.
// The original rdma-core project is available under a dual BSD/GPLv2 license.
// The entire project, including this file, is licensed under the GNU General Public License v2.0.
// For details on the licensing of the code derived from ud_pingpong.c,
// please refer to the comments in the rdma.go file.

package main

/*
#cgo LDFLAGS: -libverbs
#include <infiniband/verbs.h>
#include <stdlib.h>
*/
import "C"

import (
	"flag"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"runtime"
	"strings"
	"time"
	"unsafe" // Required for cgo pointer conversions
)

// Global options corresponding to C version arguments
var (
	port        = flag.Int("p", 18515, "TCP port for exchanging connection data")
	ibDevName   = flag.String("d", "", "IB device name")
	ibPort      = flag.Int("i", 1, "IB port")
	size        = flag.Int("s", 4096, "Size of message buffer")
	rxDepth     = flag.Int("r", 500, "RX queue depth")
	iters       = flag.Int("n", 1000, "Number of iterations")
	sl          = flag.Int("l", 0, "Service Level")
	useEvent    = flag.Bool("e", false, "Use CQ events")
	gidx        = flag.Int("g", -1, "GID index")
	validateBuf = flag.Bool("c", false, "Validate buffer contents (server side)")
	serverName  = flag.String("servername", "", "Server hostname or IP address (client mode)")
	logLevel    = flag.String("loglevel", "info", "Log level (debug, info, warn, error)")
)

func main() {
	flag.Usage = func() {
		// Usage messages should go to stderr as is standard.
		fmt.Fprintf(os.Stderr, "Usage: %s [options] [servername]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	// Initialize logger
	var programLevel = new(slog.LevelVar)
	handlerOpts := slog.HandlerOptions{Level: programLevel}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &handlerOpts))
	slog.SetDefault(logger)

	switch strings.ToLower(*logLevel) {
	case "debug":
		programLevel.Set(slog.LevelDebug)
	case "info":
		programLevel.Set(slog.LevelInfo)
	case "warn":
		programLevel.Set(slog.LevelWarn)
	case "error":
		programLevel.Set(slog.LevelError)
	default:
		slog.Error("Invalid log level specified, defaulting to info", "level", *logLevel)
		programLevel.Set(slog.LevelInfo)
	}

	// If a servername is provided as a positional argument, use it.
	// This matches the behavior of the C version.
	if flag.NArg() > 0 {
		*serverName = flag.Arg(0)
	}

	slog.Info("Starting Go RDMA UD PingPong...")
	slog.Info("Configuration",
		"Servername", *serverName,
		"Port", *port,
		"IBDevice", *ibDevName,
		"IBPort", *ibPort,
		"Size", *size,
		"RXDepth", *rxDepth,
		"Iters", *iters,
		"SL", *sl,
		"UseEvent", *useEvent,
		"GIDIndex", *gidx,
		"Validate", *validateBuf,
		"LogLevel", *logLevel)

	// 1. Get IB device list
	var numDevices C.int
	devices := C.ibv_get_device_list(&numDevices)
	if devices == nil {
		slog.Error("Failed to get IB devices list")
		os.Exit(1)
	}
	if numDevices == 0 {
		C.ibv_free_device_list(devices)
		slog.Error("No IB devices found")
		os.Exit(1)
	}
	defer C.ibv_free_device_list(devices) // Ensure list is freed

	// 2. Select IB device
	var ibDev *C.struct_ibv_device
	deviceSlice := (*[1 << 30]*C.struct_ibv_device)(unsafe.Pointer(devices))[:numDevices:numDevices]
	if *ibDevName == "" {
		ibDev = deviceSlice[0]
		if ibDev == nil {
			slog.Error("No IB devices found in list (default)")
			os.Exit(1)
		}
		slog.Info("Using default IB device", "name", C.GoString(C.ibv_get_device_name(ibDev)))
	} else {
		found := false
		for i := 0; i < int(numDevices); i++ {
			if C.GoString(C.ibv_get_device_name(deviceSlice[i])) == *ibDevName {
				ibDev = deviceSlice[i]
				found = true
				break
			}
		}
		if !found {
			slog.Error("IB device not found", "name", *ibDevName)
			os.Exit(1)
		}
		slog.Info("Using specified IB device", "name", C.GoString(C.ibv_get_device_name(ibDev)))
	}

	// 3. Initialize RDMA context
	rdmaCtx, err := initRdmaContext(ibDev, *size, *rxDepth, *ibPort, *useEvent)
	if err != nil {
		slog.Error("Failed to initialize RDMA context", "error", err)
		os.Exit(1)
	}
	defer closeRdmaContext(rdmaCtx)

	// 4. Post initial receives
	routs, err := postReceives(rdmaCtx, rdmaCtx.rxDepth)
	if err != nil || routs < rdmaCtx.rxDepth {
		slog.Error("Couldn't post initial receives", "posted", routs, "error", err)
		os.Exit(1)
	}
	slog.Info("Posted initial receive WRs", "count", routs)

	if *useEvent {
		if C.ibv_req_notify_cq(rdmaCtx.cq, 0) != 0 {
			slog.Error("Couldn't request CQ notification")
			os.Exit(1)
		}
	}

	// 5. Prepare local destination info
	myDest := &rdmaDest{
		lid: rdmaCtx.portInfo.lid,
		qpn: rdmaCtx.qp.qp_num,
		psn: C.uint32_t(rand.Intn(0xffffff + 1)), // Initial PSN for client/server to know. Actual SQ PSN set in connectUD or by RTS.
	}

	if *gidx >= 0 {
		// Pinポインタを使用
		var pinner runtime.Pinner
		pinner.Pin(&myDest.gid)

		if C.ibv_query_gid(rdmaCtx.context, C.uint8_t(*ibPort), C.int(*gidx), &myDest.gid) != 0 {
			pinner.Unpin()
			slog.Error("Could not get local GID", "gidx", *gidx)
			os.Exit(1)
		}

		pinner.Unpin()
	} else {
		// Zero out GID if not used (common for InfiniBand, non-RoCE)
		myDest.gid = C.union_ibv_gid{}
	}
	slog.Info("Local address",
		"LID", fmt.Sprintf("0x%04x", myDest.lid),
		"QPN", fmt.Sprintf("0x%06x", myDest.qpn),
		"PSN", fmt.Sprintf("0x%06x", myDest.psn),
		"GID", myDest.GidToString())

	// 6. Exchange destination info
	var remDest *rdmaDest
	if *serverName != "" { // Client mode
		remDest, err = exchangeDestClient(*serverName, *port, myDest)
	} else { // Server mode
		remDest, err = exchangeDestServer(*port, myDest, rdmaCtx, *ibPort, *sl, *gidx)
	}
	if err != nil {
		slog.Error("Failed to exchange destination info", "error", err)
		os.Exit(1)
	}
	slog.Info("Remote address",
		"LID", fmt.Sprintf("0x%04x", remDest.lid),
		"QPN", fmt.Sprintf("0x%06x", remDest.qpn),
		"PSN", fmt.Sprintf("0x%06x", remDest.psn),
		"GID", remDest.GidToString())

	// Connect UD QP (Modify to RTR/RTS and create AH)
	// Always use the gidx provided by the user
	// Server connects after exchanging dest info
	if *serverName != "" { // Client connects here
		if err := connectUD(rdmaCtx, *ibPort, myDest.psn, *sl, remDest, *gidx); err != nil {
			slog.Error("Client: Failed to connect UD", "error", err)
			os.Exit(1)
		}
	} else { // Server connects after exchange
		if err := connectUD(rdmaCtx, *ibPort, myDest.psn, *sl, remDest, *gidx); err != nil {
			slog.Error("Server: Failed to connect UD", "error", err)
			os.Exit(1)
		}
		slog.Info("Server: UD QP connected and AH created")
	}

	rdmaCtx.pending = pingpongRecvWRID // Initially, we expect a receive (especially for server)

	// Client sends the first message
	if *serverName != "" {
		if *validateBuf {
			// C version compatibility: write 0 (based on % sizeof(char)) to payload start
			payloadOffset := 40 // GRH size
			for i := 0; i < *size; i += rdmaCtx.pageSize {
				// rdmaCtx.bufSlice[i+40] = byte(i / rdmaCtx.pageSize % 256)
				// rdmaCtx.bufSlice[i] = byte(i / rdmaCtx.pageSize % 1)
				if payloadOffset+i < len(rdmaCtx.bufSlice) { // Ensure we are within bounds
					rdmaCtx.bufSlice[payloadOffset+i] = byte(i / rdmaCtx.pageSize % 1)
				}
			}
		}
		slog.Debug("Client: Attempting initial send", "RemoteQPN", fmt.Sprintf("0x%06x", uint32(remDest.qpn)), "CurrentPending", rdmaCtx.pending)
		if err := postSendUD(rdmaCtx, uint32(remDest.qpn)); err != nil {
			slog.Error("Client: Couldn't post initial send", "error", err)
			os.Exit(1)
		}
		rdmaCtx.pending |= pingpongSendWRID
		slog.Debug("Client: Initial send posted", "NewPending", rdmaCtx.pending)
	}

	start := time.Now()
	rcnt, scnt := 0, 0
	numCqEvents := 0

	// 8. Main ping-pong loop
	for rcnt < *iters || scnt < *iters {
		if *useEvent {
			var evCQ *C.struct_ibv_cq
			var evCtx unsafe.Pointer

			// Pinポインタを使用
			var pinner runtime.Pinner
			pinner.Pin(&evCQ)
			pinner.Pin(&evCtx)

			slog.Debug("CQ event get")
			if C.ibv_get_cq_event(rdmaCtx.channel, &evCQ, &evCtx) != 0 {
				pinner.Unpin()
				slog.Error("Failed to get CQ event")
				os.Exit(1)
			}
			slog.Debug("CQ event received")
			numCqEvents++
			if evCQ != rdmaCtx.cq {
				pinner.Unpin()
				slog.Error("CQ event for unknown CQ", "event_cq", evCQ, "expected_cq", rdmaCtx.cq)
				os.Exit(1)
			}
			C.ibv_ack_cq_events(rdmaCtx.cq, 1) // Ack one event at a time
			slog.Debug("CQ event acknowledged")

			if C.ibv_req_notify_cq(rdmaCtx.cq, 0) != 0 {
				pinner.Unpin()
				slog.Error("Couldn't re-request CQ notification")
				os.Exit(1)
			}

			pinner.Unpin()
			slog.Debug("Client: CQ notification re-requested")
		}

		wc := make([]C.struct_ibv_wc, 2) // Max 2 completions (one send, one recv)
		var pinner runtime.Pinner
		pinner.Pin(&wc)
		ne := 0
		ne = int(C.ibv_poll_cq(rdmaCtx.cq, 2, &wc[0]))
		pinner.Unpin()

		if ne < 0 {
			slog.Error("Poll CQ failed", "return_code", ne)
			os.Exit(1)
		}

		slog.Debug("Received completions", "count", ne)
		for i := 0; i < ne; i++ {
			if wc[i].status != C.IBV_WC_SUCCESS {
				slog.Error("Failed WC status",
					"status_str", C.GoString(C.ibv_wc_status_str(wc[i].status)),
					"status_int", int(wc[i].status),
					"wr_id", uint64(wc[i].wr_id))
				os.Exit(1)
			}

			switch wc[i].wr_id {
			case pingpongSendWRID:
				scnt++
				slog.Debug("Send completion received")
			case pingpongRecvWRID:
				slog.Debug("Receive completion received")

				routs--
				// Refill RX queue. C version refills when routs <= 1
				if routs <= 1 { // Match C version threshold
					toPost := rdmaCtx.rxDepth - routs
					if toPost > 0 { // Avoid posting 0
						postedNow, err := postReceives(rdmaCtx, toPost)
						if err != nil || postedNow < toPost {
							slog.Error("Couldn't fully replenish receives", "posted", postedNow, "requested", toPost, "error", err)
							os.Exit(1)
						}
						routs += postedNow
						slog.Debug("Receive replenished", "rcnt", rcnt, "routs", routs)
					}
				}
				rcnt++
			default:
				slog.Error("Completion for unknown WR_ID", "wr_id", uint64(wc[i].wr_id))
				os.Exit(1)
			}

			rdmaCtx.pending &= ^(int(wc[i].wr_id)) // Clear the completed WR type bit
			slog.Debug("Pending after completion", "pending", rdmaCtx.pending)

			// Post next send if needed (C version L324-L331 equivalent)
			if scnt < *iters && rdmaCtx.pending == 0 {
				slog.Debug("Main Loop: Attempting send",
					"scnt", scnt,
					"iters", *iters,
					"RemoteQPN", fmt.Sprintf("0x%06x", uint32(remDest.qpn)),
					"CurrentPending", rdmaCtx.pending)
				if err := postSendUD(rdmaCtx, uint32(remDest.qpn)); err != nil {
					slog.Error("Couldn't post send", "error", err)
					os.Exit(1)
				}
				rdmaCtx.pending = pingpongRecvWRID | pingpongSendWRID
				slog.Debug("Main Loop: Send posted", "scnt", scnt, "NewPending", rdmaCtx.pending)
			}
		}
	}

	duration := time.Since(start)

	slog.Info("PingPong test finished.")
	bytes := int64(*size) * int64(*iters) * 2 // send + recv
	slog.Info("Test results",
		"iterations", *iters,
		"bytes_per_iteration", *size,
		"total_time_sec", fmt.Sprintf("%.2f", duration.Seconds()),
		"total_bytes", bytes,
		"throughput_mbit_sec", fmt.Sprintf("%.2f", float64(bytes)*8.0/duration.Seconds()/1000000.0),
		"latency_usec_iter", fmt.Sprintf("%.2f", float64(duration.Microseconds())/float64(*iters)))

	// Validate buffer contents on server side after loop (C version L350-L354)
	if *validateBuf && *serverName == "" {
		slog.Info("Validating buffer contents...")
		payloadOffset := 40 // GRH size
		for k := 0; k < *size; k += rdmaCtx.pageSize {
			// Check within buffer bounds, accounting for offset
			if payloadOffset+k >= *size {
				break // Avoid reading past buffer end
			}
			// Expect data matching client's % sizeof(char) logic
			expected := byte((k / rdmaCtx.pageSize) % 1)
			actual := rdmaCtx.bufSlice[payloadOffset+k]
			if actual != expected {
				slog.Warn("Server validation: Mismatch in received data",
					"page", k/rdmaCtx.pageSize,
					"offset", payloadOffset+k,
					"expected", expected,
					"got", actual)
				// C version prints but doesn't exit, matching that behavior
			}
		}
		slog.Info("Buffer validation finished.")
	}

	if *useEvent {
		// Ack any remaining events (though ideally all processed in loop)
		// C version acks based on num_cq_events, which was incremented differently.
		// Safest might be to not ack here or ack based on polled completions if needed.
		C.ibv_ack_cq_events(rdmaCtx.cq, C.uint(numCqEvents))
	}

	slog.Info("Go RDMA UD PingPong finished.")
}
