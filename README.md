# go-rocev2-ud-pingpong

## Overview

go-rocev2-ud-pingpong is a network diagnostic tool that performs ping/pong tests using the Infiniband Verbs interface (libibverbs) with the RoCE v2 protocol's Unreliable Datagram (UD) transport. This tool is particularly useful for testing and benchmarking RDMA network performance in RoCE v2 environments.

This project is a Go language implementation of the [ud_pingpong.c](https://github.com/linux-rdma/rdma-core/blob/master/libibverbs/examples/ud_pingpong.c) example from the rdma-core library.

## Prerequisites

- Go 1.24 or later
- RDMA-capable network interface card supporting RoCE v2
- rdma-core development packages

## Installation

### From Source

```bash
git clone https://github.com/yuuki/go-rocev2-ud-pingpong.git
cd go-rocev2-ud-pingpong
make extract-binary
```

## Usage

The tool operates in either server (passive) or client (active) mode:

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

### Example Usage

To run in server mode:
```bash
./bin/go-rocev2-ud-pingpong -e -g 0
```

To run in client mode:
```bash
./bin/go-rocev2-ud-pingpong -e -g 0 -servername 192.168.1.100
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the GPL v2 - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- This project is inspired by and based on the [ud_pingpong.c](https://github.com/linux-rdma/rdma-core/blob/master/libibverbs/examples/ud_pingpong.c) example from the rdma-core library
- Thanks to the RDMA community for their excellent documentation and examples
