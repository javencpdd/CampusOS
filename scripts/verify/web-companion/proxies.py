"""Owned test-only HTTP CONNECT / SOCKS5 relays, never application configuration."""
import ipaddress
import select
import socket
import socketserver
import struct
import threading


def exact(client, size):
    result = bytearray()
    while len(result) < size:
        part = client.recv(size - len(result))
        if not part:
            raise ConnectionError('incomplete proxy request')
        result.extend(part)
    return bytes(result)


def relay(client, address, port):
    if not ipaddress.ip_address(address).is_global or port != 443:
        raise ValueError('test relay only accepts public literal HTTPS destinations')
    remote = socket.create_connection((address, port), timeout=8)
    remote.settimeout(20)
    return remote


def copy_tunnel(client, remote):
    with remote:
        while True:
            readable, _, _ = select.select([client, remote], [], [], 20)
            if not readable:
                return
            for source in readable:
                raw = source.recv(65536)
                if not raw:
                    return
                (remote if source is client else client).sendall(raw)


class FixtureProxies:
    def __init__(self):
        self.counts = {'http': 0, 'socks5': 0, 'server': 0}
        self.servers = []
        owner = self

        class HTTP(socketserver.BaseRequestHandler):
            def handle(self):
                self.request.settimeout(15)
                header = bytearray()
                while not header.endswith(b'\r\n\r\n'):
                    header.extend(exact(self.request, 1))
                    if len(header) > 16384:
                        raise ValueError('oversized CONNECT header')
                method, target, version = header.split(b'\r\n', 1)[0].decode('ascii').split()
                if method != 'CONNECT' or version != 'HTTP/1.1':
                    raise ValueError('not CONNECT')
                address, port = target.rsplit(':', 1)
                remote = relay(self.request, address.strip('[]'), int(port))
                owner.counts['http'] += 1
                self.request.sendall(b'HTTP/1.1 200 Connection established\r\n\r\n')
                copy_tunnel(self.request, remote)

        class SOCKS(socketserver.BaseRequestHandler):
            def handle(self):
                self.request.settimeout(15)
                version, count = exact(self.request, 2)
                if version != 5 or 0 not in exact(self.request, count):
                    raise ValueError('unexpected SOCKS authentication')
                self.request.sendall(b'\x05\x00')
                version, command, reserved, kind = exact(self.request, 4)
                if (version, command, reserved) != (5, 1, 0) or kind not in (1, 4):
                    raise ValueError('SOCKS target must be a pinned public IP')
                address = socket.inet_ntop(socket.AF_INET if kind == 1 else socket.AF_INET6, exact(self.request, 4 if kind == 1 else 16))
                port = struct.unpack('!H', exact(self.request, 2))[0]
                remote = relay(self.request, address, port)
                owner.counts['socks5'] += 1
                self.request.sendall(b'\x05\x00\x00\x01\x7f\x00\x00\x01\x00\x00')
                copy_tunnel(self.request, remote)

        class Denied(socketserver.BaseRequestHandler):
            def handle(self):
                owner.counts['server'] += 1
                self.request.sendall(b'HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n')

        class Server(socketserver.ThreadingTCPServer):
            daemon_threads = True

        self.profiles = []
        for protocol, handler in [('http', HTTP), ('socks5', SOCKS), ('server', Denied)]:
            server = Server(('127.0.0.1', 0), handler)
            threading.Thread(target=server.serve_forever, daemon=True).start()
            self.servers.append(server)
            self.profiles.append(dict(id='owned-' + protocol, label='Owned ' + protocol, url=f'{"http" if protocol=="server" else protocol}://127.0.0.1:{server.server_address[1]}'))

    def close(self):
        for server in self.servers:
            server.shutdown()
            server.server_close()
