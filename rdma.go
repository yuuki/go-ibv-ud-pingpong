// This project is licensed under the GNU General Public License v2.0.
// See the LICENSE file for more details.
//
// This file contains code derived from rdma-core/libibverbs/examples/ud_pingpong.c.
// The original rdma-core project is available under a dual BSD/GPLv2 license.
// The derived code in this file is licensed under the GNU General Public License v2.0
// as part of this project.
//
// Original copyright for derived portions:
// Copyright (c) [Year, Original Copyright Holder - e.g., The Regents of the University of California and/or other rdma-core contributors]
// Please fill in the correct copyright holder and year based on the original ud_pingpong.c file.
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are met:
//
// 1. Redistributions of source code must retain the above copyright notice,
//    this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright notice,
//    this list of conditions and the following disclaimer in the documentation
//    and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
// AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER OR CONTRIBUTORS BE
// LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
// CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
// SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
// INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
// CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
// ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
// POSSIBILITY OF SUCH DAMAGE.

package main

/*
#include <infiniband/verbs.h>
#include <stdlib.h>
#include <arpa/inet.h> // For inet_ntop
#include <sys/param.h> // For PAGE_SIZE (may need adjustment for non-Linux)
#include <stdio.h>     // For perror
#include <string.h>    // For memset
#include <errno.h>     // Add errno for error reporting
#include <netdb.h>

// Helper function to get gid string
static const char *gid_to_string(union ibv_gid *gid) {
    static char buf[INET6_ADDRSTRLEN];
    inet_ntop(AF_INET6, gid, buf, sizeof(buf));
    return buf;
}

// Helper for page size
static int get_page_size() {
	// Using sysconf(_SC_PAGESIZE) might be more portable in C,
	// but hardcoding or getting it via Go's syscall might be needed.
	// For simplicity here, assume common 4096.
	// return sysconf(_SC_PAGESIZE);
	return 4096; // Or use Go's os.Getpagesize()
}

// Helper to check if GID is zero
static int is_gid_zero(union ibv_gid *gid) {
    int i;
    for (i = 0; i < 16; i++) { // Check all 16 bytes
        if (gid->raw[i] != 0) {
            return 0; // Not zero
        }
    }
    return 1; // Is zero
}

// Helper to set send WR UD fields
static void set_ud_fields(struct ibv_send_wr *swr, struct ibv_ah *ah, uint32_t remote_qpn, uint32_t remote_qkey) {
    swr->wr.ud.ah = ah;
    swr->wr.ud.remote_qpn = remote_qpn;
    swr->wr.ud.remote_qkey = remote_qkey;
}

// Helper to query port - This addresses the compatibility issue with ibv_query_port
static int safe_query_port(struct ibv_context *context, uint8_t port_num, struct ibv_port_attr *port_attr) {
    return ibv_query_port(context, port_num, port_attr);
}

struct pingpong_dest {
	int lid;
	int qpn;
	int psn;
	union ibv_gid gid;
};

// Helper to connect UD QP and create AH
static int connect_ud_helper(struct ibv_context *context, struct ibv_pd *pd, struct ibv_qp *qp,
                              int port_num, int sl, uint32_t my_psn,
                              struct pingpong_dest *dest, int sgid_idx,
                              struct ibv_ah **ah_out) { // Pass ah_out to return the created AH

    struct ibv_qp_attr attr;
    struct ibv_ah_attr ah_attr;

    // Additional check for LID - problematic if zero with IB (not RoCEv2)
    if (dest->lid == 0) {
        fprintf(stderr, "WARNING: dest->lid is 0, which may be invalid for IB fabrics.\n");

        // Query port to check if we're on Ethernet or InfiniBand
        struct ibv_port_attr port_attr;
        if (ibv_query_port(context, port_num, &port_attr) != 0) {
            fprintf(stderr, "Failed to query port attributes\n");
        } else {
            fprintf(stderr, "Port %d link layer: %d (%s)\n",
                port_num, port_attr.link_layer,
                port_attr.link_layer == IBV_LINK_LAYER_INFINIBAND ? "InfiniBand" :
                port_attr.link_layer == IBV_LINK_LAYER_ETHERNET ? "Ethernet" : "Other");

            if (port_attr.link_layer == IBV_LINK_LAYER_INFINIBAND && dest->lid == 0) {
                fprintf(stderr, "ERROR: LID is 0 on InfiniBand fabric - invalid configuration\n");
            }
        }
    }

    // Modify QP to RTR
    memset(&attr, 0, sizeof(attr));
    attr.qp_state = IBV_QPS_RTR;
    if (ibv_modify_qp(qp, &attr, IBV_QP_STATE)) {
        perror("Failed to modify QP to RTR"); // Use perror for C errors
        return 1;
    }

    // Modify QP to RTS
    memset(&attr, 0, sizeof(attr));
    attr.qp_state = IBV_QPS_RTS;
    attr.sq_psn = my_psn;
    if (ibv_modify_qp(qp, &attr, IBV_QP_STATE | IBV_QP_SQ_PSN)) {
        perror("Failed to modify QP to RTS");
        return 1;
    }

    // Create Address Handle (AH)
    memset(&ah_attr, 0, sizeof(ah_attr));
    ah_attr.dlid = dest->lid;
    ah_attr.sl = sl;
    ah_attr.src_path_bits = 0;
    ah_attr.port_num = port_num;

    fprintf(stderr, "DEBUG: ah_attr setup:\n  dlid: 0x%04x, sl: %d, port_num: %d\n",
            ah_attr.dlid, ah_attr.sl, ah_attr.port_num);

    // Check if GID is set (RoCE) using is_gid_zero helper
    if (dest->gid.global.interface_id) { // Check if *not* zero
        ah_attr.is_global = 1;
        ah_attr.grh.hop_limit = 1;
        ah_attr.grh.dgid = dest->gid; // Direct assignment works for unions
        ah_attr.grh.sgid_index = sgid_idx;
    }

    *ah_out = ibv_create_ah(pd, &ah_attr); // Assign the created AH to the output parameter
    if (!(*ah_out)) {
        int err = errno; // Save errno
        return 1;
    }

    return 0; // Success
}

static inline int c_post_send_ud_impl(
    struct ibv_qp* qp,
    struct ibv_mr* mr,
    void* go_buf_ptr,      // Pointer to the start of the Go buffer (e.g., ctx.buf)
    int offset,            // Offset into the buffer (e.g., 40)
    int sge_length,        // Length of the data for SGE (e.g., ctx.size)
    uint32_t send_flags,   // Send flags (e.g., ctx.sendFlags)
    struct ibv_ah* ah,     // Address Handle
    uint32_t remote_qpn,   // Remote QPN
    uint32_t remote_qkey,  // Remote QKey (e.g., 0x11111111)
    uint64_t wr_id         // Work Request ID (e.g., pingpongSendWRID)
) {
    struct ibv_sge sge;
    sge.addr   = (uint64_t)((char*)go_buf_ptr + offset); // Apply offset to buffer address
    sge.lkey   = mr->lkey;
    sge.length = (uint32_t)sge_length;

    struct ibv_send_wr swr;
    memset(&swr, 0, sizeof(swr)); // Initialize swr to zeros
    swr.wr_id      = wr_id;
    swr.next       = NULL;
    swr.sg_list    = &sge;
    swr.num_sge    = 1;
    swr.opcode     = IBV_WR_SEND; // Standard IBV_WR_SEND opcode
    swr.send_flags = send_flags;

    // Populate the UD-specific fields of the Work Request
    swr.wr.ud.ah          = ah;
    swr.wr.ud.remote_qpn  = remote_qpn;
    swr.wr.ud.remote_qkey = remote_qkey;

    struct ibv_send_wr *bad_wr = NULL;
    if (ibv_post_send(qp, &swr, &bad_wr) != 0) {
        perror("ibv_post_send in c_post_send_ud_impl"); // Use perror to print error details
        return 1; // Indicate failure
    }
    return 0; // Indicate success
}
*/
import "C"

import (
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"runtime"
	"time"
	"unsafe"
)

// Corresponds to pingpong_context in C
type rdmaContext struct {
	context   *C.struct_ibv_context
	channel   *C.struct_ibv_comp_channel
	pd        *C.struct_ibv_pd
	mr        *C.struct_ibv_mr
	cq        *C.struct_ibv_cq
	qp        *C.struct_ibv_qp
	ah        *C.struct_ibv_ah // Address Handle for UD send
	buf       unsafe.Pointer   // Memory buffer (use unsafe.Pointer for C interaction)
	bufSlice  []byte           // Go slice pointing to the buffer
	size      int
	sendFlags C.int
	rxDepth   int
	pending   int // Tracks pending WR IDs
	portInfo  C.struct_ibv_port_attr
	pageSize  int
	useEvent  bool
	r         *rand.Rand
}

// Corresponds to pingpong_dest in C
type rdmaDest struct {
	lid C.uint16_t
	qpn C.uint32_t
	psn C.uint32_t
	gid C.union_ibv_gid
}

const (
	pingpongRecvWRID = 1
	pingpongSendWRID = 2
)

// pp_init_ctx equivalent
func initRdmaContext(ibDev *C.struct_ibv_device, size, rxDepth, ibPort int, useEvent bool) (*rdmaContext, error) {
	ctx := &rdmaContext{
		size:     size,
		rxDepth:  rxDepth,
		pageSize: os.Getpagesize(),
		useEvent: useEvent,
	}

	// 1. Open Device first to get context
	ctx.context = C.ibv_open_device(ibDev)
	if ctx.context == nil {
		return nil, fmt.Errorf("failed to open device %s", C.GoString(C.ibv_get_device_name(ibDev)))
	}

	// 2. Query Port Info using the context
	// Use our safe wrapper for ibv_query_port to avoid compatibility issues
	var portInfo C.struct_ibv_port_attr
	if C.safe_query_port(ctx.context, C.uint8_t(ibPort), &portInfo) != 0 {
		C.ibv_close_device(ctx.context)
		return nil, fmt.Errorf("failed to query port %d info", ibPort)
	}
	ctx.portInfo = portInfo

	// 3. Create Completion Channel (if needed)
	if useEvent {
		ctx.channel = C.ibv_create_comp_channel(ctx.context)
		if ctx.channel == nil {
			C.ibv_close_device(ctx.context)
			return nil, fmt.Errorf("failed to create completion channel")
		}
	}

	// 4. Allocate PD
	ctx.pd = C.ibv_alloc_pd(ctx.context)
	if ctx.pd == nil {
		if ctx.channel != nil {
			C.ibv_destroy_comp_channel(ctx.channel)
		}
		C.ibv_close_device(ctx.context)
		return nil, fmt.Errorf("failed to allocate protection domain")
	}

	// Allocate buffer aligned to page size
	bufC := C.aligned_alloc(C.size_t(ctx.pageSize), C.size_t(ctx.size+40))
	if bufC == nil {
		// Proper cleanup in reverse order
		C.ibv_dealloc_pd(ctx.pd)
		if ctx.channel != nil {
			C.ibv_destroy_comp_channel(ctx.channel)
		}
		C.ibv_close_device(ctx.context)
		return nil, fmt.Errorf("failed to allocate memory buffer")
	}
	ctx.buf = bufC
	// Create a Go slice that references the C memory. Be careful with lifetimes!
	ctx.bufSlice = (*[1 << 30]byte)(ctx.buf)[: ctx.size+40 : ctx.size+40]

	// Register memory region
	access := C.IBV_ACCESS_LOCAL_WRITE | C.IBV_ACCESS_REMOTE_WRITE
	ctx.mr = C.ibv_reg_mr(ctx.pd, ctx.buf, C.size_t(ctx.size+40), C.int(access))
	if ctx.mr == nil {
		C.free(ctx.buf)
		C.ibv_dealloc_pd(ctx.pd)
		if ctx.channel != nil {
			C.ibv_destroy_comp_channel(ctx.channel)
		}
		C.ibv_close_device(ctx.context)
		return nil, fmt.Errorf("failed to register memory region")
	}

	// Create CQ
	cqEntries := C.int(rxDepth + 1)
	ctx.cq = C.ibv_create_cq(ctx.context, cqEntries, nil, ctx.channel, 0)
	if ctx.cq == nil {
		C.ibv_dereg_mr(ctx.mr)
		C.free(ctx.buf)
		C.ibv_dealloc_pd(ctx.pd)
		if ctx.channel != nil {
			C.ibv_destroy_comp_channel(ctx.channel)
		}
		C.ibv_close_device(ctx.context)
		return nil, fmt.Errorf("failed to create completion queue")
	}

	// Create UD QP
	qpInitAttr := C.struct_ibv_qp_init_attr{
		send_cq: ctx.cq,
		recv_cq: ctx.cq,
		cap: C.struct_ibv_qp_cap{
			max_send_wr:  1,
			max_recv_wr:  C.uint32_t(rxDepth),
			max_send_sge: 1,
			max_recv_sge: 1,
		},
		qp_type: C.IBV_QPT_UD,
	}
	ctx.qp = C.ibv_create_qp(ctx.pd, &qpInitAttr)
	if ctx.qp == nil {
		C.ibv_destroy_cq(ctx.cq)
		C.ibv_dereg_mr(ctx.mr)
		C.free(ctx.buf)
		C.ibv_dealloc_pd(ctx.pd)
		if ctx.channel != nil {
			C.ibv_destroy_comp_channel(ctx.channel)
		}
		C.ibv_close_device(ctx.context)
		return nil, fmt.Errorf("failed to create queue pair")
	}

	// Modify QP to INIT
	qpAttr := C.struct_ibv_qp_attr{
		qp_state:   C.IBV_QPS_INIT,
		pkey_index: 0,
		port_num:   C.uint8_t(ibPort),
		qkey:       0x11111111,
	}
	flags := C.IBV_QP_STATE | C.IBV_QP_PKEY_INDEX | C.IBV_QP_PORT | C.IBV_QP_QKEY
	if C.ibv_modify_qp(ctx.qp, &qpAttr, C.int(flags)) != 0 {
		C.ibv_destroy_qp(ctx.qp)
		C.ibv_destroy_cq(ctx.cq)
		C.ibv_dereg_mr(ctx.mr)
		C.free(ctx.buf)
		C.ibv_dealloc_pd(ctx.pd)
		if ctx.channel != nil {
			C.ibv_destroy_comp_channel(ctx.channel)
		}
		C.ibv_close_device(ctx.context)
		return nil, fmt.Errorf("failed to modify QP to INIT state")
	}

	// Seed random number generator for PSN
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	ctx.r = r

	ctx.sendFlags = C.IBV_SEND_SIGNALED

	return ctx, nil
}

// pp_post_recv equivalent
func postReceives(ctx *rdmaContext, count int) (int, error) {
	if count == 0 {
		return 0, nil
	}
	sge := C.struct_ibv_sge{
		addr:   C.uint64_t(uintptr(ctx.buf)),
		length: C.uint32_t(ctx.size) + 40,
		lkey:   ctx.mr.lkey,
	}
	rwr := C.struct_ibv_recv_wr{
		wr_id:   pingpongRecvWRID,
		next:    nil,
		sg_list: &sge,
		num_sge: 1,
	}

	// Pinポインタを使用
	var pinner runtime.Pinner
	pinner.Pin(&sge)
	pinner.Pin(&rwr)

	var badWr *C.struct_ibv_recv_wr
	posted := 0
	for i := 0; i < count; i++ {
		if C.ibv_post_recv(ctx.qp, &rwr, &badWr) != 0 {
			// Log error, but return how many were successfully posted before failure
			fmt.Fprintf(os.Stderr, "Error posting receive WR %d: %v\n", i, os.Stderr) // Crude error
			pinner.Unpin()                                                            // 忘れずにUnpin
			return posted, fmt.Errorf("ibv_post_recv failed on iteration %d", i)
		}
		posted++
	}

	// 使い終わったらUnpin
	pinner.Unpin()
	return posted, nil
}

// pp_connect_ctx equivalent (simplified for UD setup)
// Needs remote dest info to create AH
func connectUD(ctx *rdmaContext, ibPort int, myPsn C.uint32_t, sl int, dest *rdmaDest, sgidIndex int) error {
	var cAh *C.struct_ibv_ah // Variable to receive the AH pointer from C

	// デバッグ情報を追加
	slog.Debug("connectUD called with parameters", map[string]interface{}{
		"ibPort":    ibPort,
		"sl":        sl,
		"sgidIndex": sgidIndex,
		"dest_lid":  fmt.Sprintf("0x%04x", dest.lid),
		"dest_qpn":  fmt.Sprintf("0x%06x", dest.qpn),
		"dest_psn":  fmt.Sprintf("0x%06x", dest.psn),
		"dest_gid":  dest.GidToString(),
	})

	pingpongDest := C.struct_pingpong_dest{
		lid: C.int(dest.lid), // Cast to C.int
		qpn: C.int(dest.qpn), // Cast to C.int
		psn: C.int(dest.psn), // Cast to C.int
		gid: dest.gid,
	}
	// Pin destination GID for C access
	var pinner runtime.Pinner
	pinner.Pin(&pingpongDest) // Pin the whole dest struct

	// Call the C helper function
	ret := C.connect_ud_helper(ctx.context, ctx.pd, ctx.qp,
		C.int(ibPort), C.int(sl), myPsn,
		&pingpongDest, C.int(sgidIndex), // Pass address of pingpongDest
		&cAh) // Pass address of cAh to receive the pointer

	pinner.Unpin() // Unpin after C call returns

	if ret != 0 {
		// Use errno or retrieve specific error message if possible in the future
		// For now, return a generic error based on C function's print
		return fmt.Errorf("C.connect_ud_helper failed (check stderr for details)")
	}

	// Assign the created AH to the context
	ctx.ah = cAh

	slog.Debug("connectUD successful, AH created", map[string]interface{}{
		"ibPort":    ibPort,
		"sl":        sl,
		"sgidIndex": sgidIndex,
		"dest_lid":  fmt.Sprintf("0x%04x", dest.lid),
		"dest_qpn":  fmt.Sprintf("0x%06x", dest.qpn),
	})
	return nil
}

// pp_post_send equivalent for UD
func postSendUD(ctx *rdmaContext, remoteQPN uint32) error {
	const remoteQKey uint32 = 0x11111111 // As used in the original C.set_ud_fields call
	const bufferOffset = 40              // The offset used for sge.addr

	// Assuming pingpongSendWRID is defined as a Go constant/variable (e.g., const pingpongSendWRID uint64 = 1)
	// ctx.buf is assumed to be unsafe.Pointer or a type that can be cast to unsafe.Pointer.
	// For example, if ctx.buf were []byte, it would be passed as unsafe.Pointer(&ctx.buf[0]).
	ret := C.c_post_send_ud_impl(
		ctx.qp, ctx.mr, ctx.buf, C.int(bufferOffset), C.int(ctx.size),
		C.uint32_t(ctx.sendFlags), ctx.ah, C.uint32_t(remoteQPN),
		C.uint32_t(remoteQKey), C.uint64_t(pingpongSendWRID))

	if ret != 0 {
		// perror in the C function has already printed detailed error info to stderr.
		return fmt.Errorf("c_post_send_ud_impl failed (see stderr for details from perror)")
	}

	return nil
}

// pp_close_ctx equivalent
func closeRdmaContext(ctx *rdmaContext) error {
	var lastErr error
	if ctx.qp != nil && C.ibv_destroy_qp(ctx.qp) != 0 {
		err := fmt.Errorf("failed to destroy QP")
		fmt.Fprintln(os.Stderr, err)
		lastErr = err
	}
	if ctx.ah != nil && C.ibv_destroy_ah(ctx.ah) != 0 {
		err := fmt.Errorf("failed to destroy AH")
		fmt.Fprintln(os.Stderr, err)
		lastErr = err
	}
	if ctx.cq != nil && C.ibv_destroy_cq(ctx.cq) != 0 {
		err := fmt.Errorf("failed to destroy CQ")
		fmt.Fprintln(os.Stderr, err)
		lastErr = err
	}
	if ctx.mr != nil && C.ibv_dereg_mr(ctx.mr) != 0 {
		err := fmt.Errorf("failed to deregister MR")
		fmt.Fprintln(os.Stderr, err)
		lastErr = err
	}
	if ctx.pd != nil && C.ibv_dealloc_pd(ctx.pd) != 0 {
		err := fmt.Errorf("failed to deallocate PD")
		fmt.Fprintln(os.Stderr, err)
		lastErr = err
	}
	if ctx.channel != nil && C.ibv_destroy_comp_channel(ctx.channel) != 0 {
		err := fmt.Errorf("failed to destroy Comp Channel")
		fmt.Fprintln(os.Stderr, err)
		lastErr = err
	}
	if ctx.buf != nil {
		C.free(ctx.buf)
		ctx.buf = nil // Avoid double free
	}
	if ctx.context != nil && C.ibv_close_device(ctx.context) != 0 {
		err := fmt.Errorf("failed to close device context")
		fmt.Fprintln(os.Stderr, err)
		lastErr = err
	}

	return lastErr
}

// GidToString helper to get GID string from Go struct
func (dest *rdmaDest) GidToString() string {
	// Need to pass the address of the GID union within the Go struct to C
	// Pinポインタを使用
	var pinner runtime.Pinner
	pinner.Pin(&dest.gid)

	result := C.GoString(C.gid_to_string(&dest.gid))

	// 使い終わったらUnpin
	pinner.Unpin()
	return result
}
