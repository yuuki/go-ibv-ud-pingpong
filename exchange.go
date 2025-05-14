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
#include <infiniband/verbs.h>
#include <arpa/inet.h>
#include <string.h> // for memcpy
#include <stdlib.h> // for free
#include <stdio.h>  // for sprintf

// Helper to convert GID to wire format string
static void gid_to_wire_gid(const union ibv_gid *gid, char *wire_gid) {
    uint8_t *p = (uint8_t*)gid;
    int i;
    for (i = 0; i < 16; ++i) {
        sprintf(wire_gid + i * 2, "%02x", p[i]);
    }
    wire_gid[32] = '\0';
}

// Helper to convert wire format string to GID
static void wire_gid_to_gid(const char *wire_gid, union ibv_gid *gid) {
    char tmp[3];
    int i;
    uint8_t *p = (uint8_t*)gid;
    tmp[2] = '\0';
    for (i = 0; i < 16; ++i) {
        tmp[0] = wire_gid[i * 2];
        tmp[1] = wire_gid[i * 2 + 1];
        p[i] = (uint8_t)strtol(tmp, NULL, 16);
    }
}
*/
import "C"

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"unsafe"
)

// Format string for exchanging destination info
// LID (hex, 4), QPN (hex, 6), PSN (hex, 6), GID (hex, 32)
const destMsgFormat = "%04x:%06x:%06x:%s"
const expectedMsgLen = 4 + 1 + 6 + 1 + 6 + 1 + 32 // Length of the formatted string

// exchangeDestClient connects to the server, sends its dest info, and receives the server's dest info.
func exchangeDestClient(serverAddr string, tcpPort int, myDest *rdmaDest) (*rdmaDest, error) {
	connAddr := fmt.Sprintf("%s:%d", serverAddr, tcpPort)
	conn, err := net.Dial("tcp", connAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't connect to %s: %w", connAddr, err)
	}
	defer conn.Close()

	// Prepare local destination string
	myGidWire := make([]byte, 33) // 32 hex chars + null terminator for C

	// Pinポインターを使用
	var pinner runtime.Pinner
	pinner.Pin(&myDest.gid)
	pinner.Pin(&myGidWire[0])

	C.gid_to_wire_gid(&myDest.gid, (*C.char)(unsafe.Pointer(&myGidWire[0])))
	myMsg := fmt.Sprintf(destMsgFormat, myDest.lid, myDest.qpn, myDest.psn, string(myGidWire[:32]))

	// 使い終わったらUnpin
	pinner.Unpin()

	// Send local destination info
	n, err := conn.Write([]byte(myMsg))
	if err != nil || n != len(myMsg) {
		return nil, fmt.Errorf("couldn't send local address (wrote %d/%d): %w", n, len(myMsg), err)
	}

	// Receive remote destination info
	remMsgBytes := make([]byte, expectedMsgLen)
	n, err = conn.Read(remMsgBytes)
	if err != nil || n != expectedMsgLen {
		return nil, fmt.Errorf("couldn't read remote address (read %d/%d): %w", n, expectedMsgLen, err)
	}

	// Send 'done' signal
	_, err = conn.Write([]byte("done"))
	if err != nil {
		return nil, fmt.Errorf("couldn't write done signal: %w", err)
	}

	// Parse remote destination string
	return parseDestString(string(remMsgBytes))
}

// exchangeDestServer waits for a client connection, receives its dest info, sends local dest info.
func exchangeDestServer(tcpPort int, myDest *rdmaDest, rdmaCtx *rdmaContext, ibPort int, sl int, sgidIndex int) (*rdmaDest, error) {
	listenAddr := fmt.Sprintf(":%d", tcpPort)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't listen on port %d: %w", tcpPort, err)
	}
	defer listener.Close()

	fmt.Printf("Waiting for connection on port %d...\n", tcpPort)
	conn, err := listener.Accept()
	if err != nil {
		return nil, fmt.Errorf("failed to accept connection: %w", err)
	}
	defer conn.Close()
	fmt.Printf("Accepted connection from %s\n", conn.RemoteAddr())

	// Receive remote (client) destination info
	remMsgBytes := make([]byte, expectedMsgLen)
	n, err := conn.Read(remMsgBytes)
	if err != nil || n != expectedMsgLen {
		return nil, fmt.Errorf("couldn't read remote address (read %d/%d): %w", n, expectedMsgLen, err)
	}

	// Parse remote destination string
	remDest, err := parseDestString(string(remMsgBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to parse client dest string: %w", err)
	}

	// Prepare local destination string
	myGidWire := make([]byte, 33)

	// Pinポインターを使用
	var pinner runtime.Pinner
	pinner.Pin(&myDest.gid)
	pinner.Pin(&myGidWire[0])

	C.gid_to_wire_gid(&myDest.gid, (*C.char)(unsafe.Pointer(&myGidWire[0])))
	myMsg := fmt.Sprintf(destMsgFormat, myDest.lid, myDest.qpn, myDest.psn, string(myGidWire[:32]))

	// 使い終わったらUnpin
	pinner.Unpin()

	// Connect UD QP for server - Moved to main.go after exchange
	// if err := connectUD(rdmaCtx, ibPort, myDest.psn, sl, remDest, sgidIndex); err != nil {
	// 	return nil, fmt.Errorf("Server: Failed to connect UD: %v", err)
	// }
	// fmt.Println("Server: UD QP connected and AH created")

	// Send local destination info
	n, err = conn.Write([]byte(myMsg))
	if err != nil || n != len(myMsg) {
		return nil, fmt.Errorf("couldn't send local address (wrote %d/%d): %w", n, len(myMsg), err)
	}

	// Wait for 'done' signal (optional, but matches C version)
	doneBuf := make([]byte, 4)
	_, err = conn.Read(doneBuf)
	if err != nil {
		// Don't treat EOF as critical error here if client just closes
		fmt.Fprintf(os.Stderr, "Warning: error reading 'done' signal: %v\n", err)
	}

	return remDest, nil
}

// parseDestString parses the destination string format.
func parseDestString(msg string) (*rdmaDest, error) {
	parts := strings.Split(msg, ":")
	if len(parts) != 4 {
		return nil, fmt.Errorf("invalid destination format: expected 4 parts, got %d (%s)", len(parts), msg)
	}

	lid, err := strconv.ParseUint(parts[0], 16, 16)
	if err != nil {
		return nil, fmt.Errorf("invalid LID '%s': %w", parts[0], err)
	}
	qpn, err := strconv.ParseUint(parts[1], 16, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid QPN '%s': %w", parts[1], err)
	}
	psn, err := strconv.ParseUint(parts[2], 16, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid PSN '%s': %w", parts[2], err)
	}
	gidStr := parts[3]
	if len(gidStr) != 32 {
		return nil, fmt.Errorf("invalid GID string length: %d != 32", len(gidStr))
	}

	dest := &rdmaDest{
		lid: C.uint16_t(lid),
		qpn: C.uint32_t(qpn),
		psn: C.uint32_t(psn),
	}

	// Convert wire GID string back to C GID struct
	cs := C.CString(gidStr)

	// Pinポインターを使用
	var pinner runtime.Pinner
	pinner.Pin(cs)
	pinner.Pin(&dest.gid)

	C.wire_gid_to_gid(cs, &dest.gid)

	// CSの解放前にUnpin
	pinner.Unpin()
	C.free(unsafe.Pointer(cs))

	return dest, nil
}
