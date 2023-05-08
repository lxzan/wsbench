# wsbench

### install
```bash
go install github.com/lxzan/wsbench@latest
```

### help
```
Usage of ./bin/wsbench:
  -c int
        num of client (default 100)
  -compress
        compress
  -n int
        num of message per connection (default 100)
  -p int
        payload size (default 4096)
  -u string
        url (default "ws://127.0.0.1/")
```

### example
```
$ wsbench -u 'ws://127.0.0.1:8000' -c 10 -n 10000

IOPS: 772833
Cost: 129.41ms
P50:  2.07ms
P90:  15.91ms
P99:  27.36ms
```