# Sample TCP Server

This is a simple TCP server that listens on the specified port (3490 by default) and replies to incoming connections by providing info about src/destination ip/port.

It demonstrates the use of the getsockopt() system call with the ORIGINAL_DST option to retrieve the original destination ip/port after an iptables redirect.

So, for example, if you have the server listening on port 3490 on the local machine and an iptables rule like:

```
iptables -t nat -I OUTPUT 1 -p tcp --dport 4000:5000 -j REDIRECT --to-port 3490
```
your will see:

```
$ telnet localhost 3490
FROM 127.0.0.1:44978, TO 127.0.0.1:3490, ORIG DEST 127.0.0.1:3490

$ telnet localhost 4100
FROM 127.0.0.1:35476, TO 127.0.0.1:3490, ORIG DEST 127.0.0.1:4100

$ telnet 1.1.1.1 5000
FROM 100.100.100.100:60275, TO 127.0.0.1:3490, ORIG DEST 1.1.1.1:5000
```


