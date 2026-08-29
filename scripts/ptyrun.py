import fcntl, os, pty, select, struct, subprocess, sys, termios, time

master, slave = pty.openpty()
fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 140, 0, 0))
p = subprocess.Popen([sys.argv[1]], stdin=slave, stdout=slave, stderr=slave, close_fds=True)
os.close(slave)
out = b""
start = time.time()
deadline = start + float(sys.argv[3]) if len(sys.argv) > 3 else start + 4
keys = sys.argv[4] if len(sys.argv) > 4 else ""
key_idx = 0
next_key_time = start + 2
while time.time() < deadline:
    r, _, _ = select.select([master], [], [], 0.1)
    if master in r:
        try:
            chunk = os.read(master, 65536)
        except OSError:
            break
        if not chunk:
            break
        out += chunk
    if key_idx < len(keys) and time.time() >= next_key_time:
        try:
            os.write(master, keys[key_idx].encode())
        except OSError:
            break
        key_idx += 1
        next_key_time += 0.15
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
