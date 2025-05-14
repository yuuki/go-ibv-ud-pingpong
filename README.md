# go-rocev2-ud-pingpong

## Overview

This is a network diagnostic tool that performs ping/pong using the Infiniband Verbs interface (libibverbs) with the RoCE v2 protocol UD (Unreliable Datagram).
It is a Go language port of [@https://github.com/linux-rdma/rdma-core/blob/master/libibverbs/examples/ud_pingpong.c](https://github.com/linux-rdma/rdma-core/blob/master/libibverbs/examples/ud_pingpong.c).

## How to Use

```
Usage: ./go-rocev2-ud-pingpong [options] [servername]
  -c    Validate buffer contents (server side)
  -d string
        IB device name
  -e    Use CQ events
  -g int
        GID index (default -1)
  -i int
        IB port (default 1)
  -l int
        Service Level
  -loglevel string
        Log level (debug, info, warn, error) (default "info")
  -n int
        Number of iterations (default 1000)
  -p int
        TCP port for exchanging connection data (default 18515)
  -r int
        RX queue depth (default 500)
  -s int
        Size of message buffer (default 4096)
  -servername string
        Server hostname or IP address (client mode)
```

```shell-session
# Passive side
$ ./bin/go-rocev2-ud-pingpong -e -g 0

# Active side
$ ./bin/go-rocev2-ud-pingpong -e -g 0 -servername <passive-side-ip-address>
```

## How to Build

```bash
make extract-binary
```

## How to Run

### Normal Execution

```bash
# After building
./bin/go-rocev2-ud-pingpong [options]


# Or using go run
go run main.go [options]
```
For options, please refer to the tool's help.


## License

This project is licensed under the GPL v2 - see the [LICENSE](LICENSE) file for details.
