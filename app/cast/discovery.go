package cast

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
)

var searchTargets = []string{
	"urn:schemas-upnp-org:device:MediaRenderer:1",
	"urn:schemas-upnp-org:service:AVTransport:1",
	"upnp:rootdevice",
	"ssdp:all",
}

type discoveryLink struct {
	network net.Interface
	ip      net.IP
}

func discoveryLinks(interfaces []net.Interface, addresses func(net.Interface) ([]net.Addr, error)) []discoveryLink {
	links := make([]discoveryLink, 0, 4)
	seen := map[string]bool{}
	for _, network := range interfaces {
		if network.Flags&net.FlagUp == 0 || network.Flags&net.FlagLoopback != 0 || network.Flags&net.FlagMulticast == 0 {
			continue
		}
		entries, err := addresses(network)
		if err != nil {
			continue
		}
		for _, address := range entries {
			ip, _, err := net.ParseCIDR(address.String())
			if err != nil || ip.To4() == nil || !ip.IsPrivate() || ip.IsLoopback() || seen[ip.String()] {
				continue
			}
			seen[ip.String()] = true
			links = append(links, discoveryLink{network, ip.To4()})
			if len(links) == 16 {
				return links
			}
		}
	}
	return links
}

// Bind each LAN IPv4 explicitly: a default route may point to mobile data,
// a VPN or an inactive virtual adapter instead of the Wi-Fi used by the TV.
func discoverySockets(group *net.UDPAddr) ([]*net.UDPConn, error) {
	if !group.IP.IsMulticast() {
		socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
		if err != nil {
			return nil, err
		}
		return []*net.UDPConn{socket}, nil
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("读取局域网接口失败: %w", err)
	}
	links := discoveryLinks(interfaces, func(network net.Interface) ([]net.Addr, error) { return network.Addrs() })
	sockets := make([]*net.UDPConn, 0, len(links))
	var lastErr error
	for _, link := range links {
		socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: link.ip})
		if err != nil {
			lastErr = err
			continue
		}
		packet := ipv4.NewPacketConn(socket)
		if err = packet.SetMulticastInterface(&link.network); err == nil {
			err = packet.SetMulticastTTL(2)
		}
		if err != nil {
			lastErr = err
			_ = socket.Close()
			continue
		}
		sockets = append(sockets, socket)
	}
	if len(sockets) == 0 {
		if lastErr != nil {
			return nil, fmt.Errorf("无法在局域网接口搜索投屏设备: %w", lastErr)
		}
		return nil, errors.New("没有可用的 IPv4 局域网，请连接电视所在的 Wi-Fi 后重试")
	}
	return sockets, nil
}

type discoveryPacket struct {
	data string
	peer net.IP
}
type discoveryJob struct {
	location string
	peer     net.IP
}

// The destination is an integration-test seam. Production always uses SSDP's
// standard multicast group; endpoint validation remains pinned to the sender.
func (s *Service) discoverAt(group *net.UDPAddr) ([]Device, error) {
	return s.discoverAtContext(context.Background(), group)
}
func (s *Service) discoverAtContext(request context.Context, group *net.UDPAddr) ([]Device, error) {
	if err := request.Err(); err != nil {
		return nil, err
	}
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	if !s.scanMu.TryLock() {
		return nil, errors.New("正在搜索设备，请稍候")
	}
	defer s.scanMu.Unlock()
	ctx, cancel := context.WithTimeout(request, 8*time.Second)
	defer cancel()
	stopOwner := context.AfterFunc(s.ctx, cancel)
	defer stopOwner()
	sockets, err := discoverySockets(group)
	if err != nil {
		return nil, err
	}
	// A separate receive lifetime lets HTTP descriptions finish after the final
	// SSDP response, while request cancellation still interrupts both stages.
	receive, stopReceive := context.WithTimeout(ctx, 4500*time.Millisecond)
	defer stopReceive()
	closeSockets := func() {
		for _, socket := range sockets {
			_ = socket.Close()
		}
	}
	closeOnCancel := context.AfterFunc(receive, closeSockets)
	defer closeOnCancel()
	defer closeSockets()
	packets := make(chan discoveryPacket, 32)
	var readers sync.WaitGroup
	for _, socket := range sockets {
		readers.Add(1)
		go func(socket *net.UDPConn) {
			defer readers.Done()
			buf := make([]byte, 8192)
			for {
				n, peer, err := socket.ReadFromUDP(buf)
				if err != nil {
					return
				}
				packet := discoveryPacket{string(buf[:n]), append(net.IP(nil), peer.IP...)}
				select {
				case packets <- packet:
				case <-receive.Done():
					return
				}
			}
		}(socket)
	}
	jobs := make(chan discoveryJob, 24)
	var workers sync.WaitGroup
	for i := 0; i < 3; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				device, err := resolve(ctx, job.location, job.peer)
				if err == nil {
					s.mu.Lock()
					s.devices[device.ID] = device
					s.mu.Unlock()
				}
			}
		}()
	}
	send := func() error {
		sent := 0
		var last error
		for _, socket := range sockets {
			for _, target := range searchTargets {
				query := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 2\r\nST: " + target + "\r\n\r\n"
				if _, err := socket.WriteToUDP([]byte(query), group); err != nil {
					last = err
				} else {
					sent++
				}
			}
		}
		if sent == 0 {
			return last
		}
		return nil
	}
	sendErr := send()
	retry := time.NewTimer(1100 * time.Millisecond)
	defer retry.Stop()
	seen := map[string]bool{}
collect:
	for sendErr == nil {
		select {
		case <-receive.Done():
			break collect
		case <-retry.C:
			sendErr = send()
		case packet := <-packets:
			response, err := http.ReadResponse(bufio.NewReader(strings.NewReader(packet.data)), nil)
			if err != nil {
				continue
			}
			if response.Body != nil {
				_ = response.Body.Close()
			}
			location := strings.TrimSpace(response.Header.Get("Location"))
			if response.StatusCode != 200 || seen[location] || len(seen) >= 32 {
				continue
			}
			if _, err := endpoint(location, packet.peer); err != nil {
				continue
			}
			// Mark only admitted jobs; queue pressure must not discard later replies.
			select {
			case jobs <- discoveryJob{location, packet.peer}:
				seen[location] = true
			default:
			}
		}
	}
	stopReceive()
	closeSockets()
	readers.Wait()
	close(jobs)
	workers.Wait()
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	if err := request.Err(); err != nil {
		return nil, err
	}
	if sendErr != nil {
		return nil, sendErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Device, 0)
	for id, device := range s.devices {
		if time.Since(device.seen) > 2*time.Hour {
			delete(s.devices, id)
		} else if time.Since(device.seen) <= 2*time.Minute {
			out = append(out, device.Device)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
