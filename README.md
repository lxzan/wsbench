# wsbench

### install
```bash
go install github.com/lxzan/wsbench@latest
```

### help
```
NAME:
   wsbench - testing websocket server iops and latency

USAGE:
   wsbench [global options] command [command options] [arguments...]

COMMANDS:
   iops       
   broadcast  
   help, h    Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --help, -h  show help
```

### example

##### IOPS
```bash
wsbench iops -c 1000 -n 1000 -p 1000 -u 'ws://127.0.0.1:8000/connect,ws://127.0.0.1:8001/connect'
```

##### Broadcast
```bash
wsbench broadcast -c 1000 -n 1 -p 1000 -i 3 -u 'ws://127.0.0.1:8000/connect,ws://127.0.0.1:8001/connect'
```