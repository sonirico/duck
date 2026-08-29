import fcntl, os, pty, select, struct, subprocess, sys, termios, time

master, slave = pty.openpty()
fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 140, 0, 0))
p = subprocess.Popen([sys.argv[1]], stdin=slave, stdout=slave, stderr=slave, close_fds=True)
os.close(slave)
out = b""
deadline = time.time() + float(sys.argv[3]) if len(sys.argv) > 3 else time.time() + 4
while time.time() < deadline:
    r, _, _ = select.select([master], [], [], 0.2)
    if master in r:
        try:
            chunk = os.read(master, 65536)
        except OSError:
            break
        if not chunk:
            break
        out += chunk
try:
    os.write(master, b"q")
    time.sleep(0.3)
except OSError:
    pass
p.terminate()
try:
    p.wait(timeout=2)
except subprocess.TimeoutExpired:
    p.kill()
with open(sys.argv[2], "wb") as f:
    f.write(out)
