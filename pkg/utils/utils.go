package utils

import (
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

var log = logrus.New()

func FletcherChecksum8(inp string) uint8 {
	var ckA, ckB uint8
	for i := 0; i < len(inp); i++ {
		ckA = (ckA + inp[i]) % 0xf
		ckB = (ckB + ckA) % 0xf
	}
	return (ckB << 4) | ckA
}

func ShortHostname() (shortName string, err error) {
	var hostname string

	if filePath, ok := os.LookupEnv("RUNTIMECFG_HOSTNAME_PATH"); ok {
		dat, err := ioutil.ReadFile(filePath)
		if err != nil {
			log.WithFields(logrus.Fields{
				"filePath": filePath,
			}).Error("Failed to read file")
			return "", err
		}
		hostname = strings.TrimSuffix(string(dat), "\n")
		log.WithFields(logrus.Fields{
			"hostname": hostname,
			"file":     filePath,
		}).Debug("Hostname retrieved from a file")
	} else {
		hostname, err = os.Hostname()
		if err != nil {
			panic(err)
		}
		log.WithFields(logrus.Fields{
			"hostname": hostname,
		}).Debug("Hostname retrieved from OS")
	}
	splitHostname := strings.SplitN(hostname, ".", 2)
	shortName = splitHostname[0]
	return shortName, err
}

func EtcdShortHostname() (shortName string, err error) {
	shortHostname, err := ShortHostname()
	if err != nil {
		panic(err)
	}
	if !strings.Contains(shortHostname, "master") {
		return "", err
	}
	etcdHostname := strings.Replace(shortHostname, "master", "etcd", 1)
	return etcdHostname, err
}

func GetFirstAddr(host string) (string, error) {
	addrs, err := net.LookupHost(host)
	if err != nil {
		return "", err
	}
	return addrs[0], nil
}

func IsKubernetesHealthy(port uint16) (bool, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: transport}
	resp, err := client.Get(fmt.Sprintf("https://localhost:%d/readyz", port))
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	return string(body) == "ok", nil
}

func AlarmStabilization(cur_alrm bool, cur_defect bool, consecutive_ctr uint8, on_threshold uint8, off_threshold uint8) (bool, uint8) {
	var new_alrm bool = cur_alrm
	var threshold uint8

	if cur_alrm != cur_defect {
		consecutive_ctr++
		if cur_alrm {
			threshold = off_threshold
		} else {
			threshold = on_threshold
		}
		if consecutive_ctr >= threshold {
			new_alrm = !cur_alrm
			consecutive_ctr = 0
		}
	} else {
		consecutive_ctr = 0
	}
	return new_alrm, consecutive_ctr
}

func GetFileMd5(filePath string) (string, error) {
	var returnMD5String string
	file, err := os.Open(filePath)
	if err != nil {
		return returnMD5String, err
	}
	defer file.Close()
	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return returnMD5String, err
	}
	hashInBytes := hash.Sum(nil)[:16]
	returnMD5String = hex.EncodeToString(hashInBytes)
	return returnMD5String, nil
}

// AddressFilter is a function type to filter addresses
type AddressFilter func(netlink.Addr) bool

// RouteFilter is a function type to filter routes
type RouteFilter func(netlink.Route) bool

func getAddrs() (addrMap map[netlink.Link][]netlink.Addr, err error) {
	nlHandle, err := netlink.NewHandle(unix.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	defer nlHandle.Delete()

	links, err := nlHandle.LinkList()
	if err != nil {
		return nil, err
	}

	addrMap = make(map[netlink.Link][]netlink.Addr)
	for _, link := range links {
		addresses, err := nlHandle.AddrList(link, netlink.FAMILY_ALL)
		if err != nil {
			return nil, err
		}
		for _, address := range addresses {
			if _, ok := addrMap[link]; ok {
				addrMap[link] = append(addrMap[link], address)
			} else {
				addrMap[link] = []netlink.Addr{address}
			}
		}
	}
	log.Debugf("retrieved Address map %+v", addrMap)
	return addrMap, nil
}

func getRouteMap() (routeMap map[int][]netlink.Route, err error) {
	nlHandle, err := netlink.NewHandle(unix.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	defer nlHandle.Delete()

	routes, err := nlHandle.RouteList(nil, netlink.FAMILY_V6)
	if err != nil {
		return nil, err
	}

	routeMap = make(map[int][]netlink.Route)
	for _, route := range routes {
		if route.Protocol != unix.RTPROT_RA {
			log.Debugf("Ignoring route non Router advertisement route %+v", route)
			continue
		}
		if _, ok := routeMap[route.LinkIndex]; ok {
			routeMap[route.LinkIndex] = append(routeMap[route.LinkIndex], route)
		} else {
			routeMap[route.LinkIndex] = []netlink.Route{route}
		}
	}

	log.Debugf("Retrieved IPv6 route map %+v", routeMap)

	return routeMap, nil
}

// NonDeprecatedAddress returns true if the address is IPv6 and has a preferred lifetime of 0
func NonDeprecatedAddress(address netlink.Addr) bool {
	return !(net.IPv6len == len(address.IP) && address.PreferedLft == 0)
}

// NonDefaultRoute returns whether the passed Route is the default
func NonDefaultRoute(route netlink.Route) bool {
	return route.Dst != nil
}

// AddressesRouting takes a slice of Virtual IPs and returns a slice of configured addresses in the current network namespace that directly route to those vips. You can optionally pass an AddressFilter and/or RouteFilter to further filter down which addresses are considered
func AddressesRouting(vips []net.IP, af AddressFilter, rf RouteFilter) ([]net.IP, error) {
	addrMap, err := getAddrs()
	if err != nil {
		return nil, err
	}

	var routeMap map[int][]netlink.Route
	matches := make([]net.IP, 0)
	for link, addresses := range addrMap {
		for _, address := range addresses {
			maskPrefix, maskBits := address.Mask.Size()
			if !af(address) {
				continue
			}
			if net.IPv6len == len(address.IP) && maskPrefix == maskBits {
				if routeMap == nil {
					routeMap, err = getRouteMap()
					if err != nil {
						return nil, err
					}
				}
				if routes, ok := routeMap[link.Attrs().Index]; ok {
					for _, route := range routes {
						if !rf(route) {
							continue
						}
						routePrefix, _ := route.Dst.Mask.Size()
						log.Infof("Checking route %+v (mask %s) for address %+v", route, route.Dst.Mask, address)
						if routePrefix != 0 {
							containmentNet := net.IPNet{IP: address.IP, Mask: route.Dst.Mask}
							for _, vip := range vips {
								log.Infof("Checking whether address %s with route %s contains VIP %s", address, route, vip)
								if containmentNet.Contains(vip) {
									log.Infof("Address %s with route %s contains VIP %s", address, route, vip)
									matches = append(matches, address.IP)
								}
							}
						}
					}
				}
			} else {
				for _, vip := range vips {
					log.Infof("Checking whether address %s contains VIP %s", address, vip)
					if address.Contains(vip) {
						log.Infof("Address %s contains VIP %s", address, vip)
						matches = append(matches, address.IP)
					}
				}
			}
		}

	}
	return matches, nil
}

func PearsonHash(origin []byte, outputLength int) (hash []uint8) {
	// table for Pearson hashing from RFC 3074.
	lookupTable := [...]uint8{
		251, 175, 119, 215, 81, 14, 79, 191, 103, 49, 181, 143, 186, 157, 0,
		232, 31, 32, 55, 60, 152, 58, 17, 237, 174, 70, 160, 144, 220, 90, 57,
		223, 59, 3, 18, 140, 111, 166, 203, 196, 134, 243, 124, 95, 222, 179,
		197, 65, 180, 48, 36, 15, 107, 46, 233, 130, 165, 30, 123, 161, 209, 23,
		97, 16, 40, 91, 219, 61, 100, 10, 210, 109, 250, 127, 22, 138, 29, 108,
		244, 67, 207, 9, 178, 204, 74, 98, 126, 249, 167, 116, 34, 77, 193,
		200, 121, 5, 20, 113, 71, 35, 128, 13, 182, 94, 25, 226, 227, 199, 75,
		27, 41, 245, 230, 224, 43, 225, 177, 26, 155, 150, 212, 142, 218, 115,
		241, 73, 88, 105, 39, 114, 62, 255, 192, 201, 145, 214, 168, 158, 221,
		148, 154, 122, 12, 84, 82, 163, 44, 139, 228, 236, 205, 242, 217, 11,
		187, 146, 159, 64, 86, 239, 195, 42, 106, 198, 118, 112, 184, 172, 87,
		2, 173, 117, 176, 229, 247, 253, 137, 185, 99, 164, 102, 147, 45, 66,
		231, 52, 141, 211, 194, 206, 246, 238, 56, 110, 78, 248, 63, 240, 189,
		93, 92, 51, 53, 183, 19, 171, 72, 50, 33, 104, 101, 69, 8, 252, 83, 120,
		76, 135, 85, 54, 202, 125, 188, 213, 96, 235, 136, 208, 162, 129, 190,
		132, 156, 38, 47, 1, 7, 254, 24, 4, 216, 131, 89, 21, 28, 133, 37, 153,
		149, 80, 170, 68, 6, 169, 234, 151,
	}

	h := uint8(0)

	for i := 0; i < outputLength; i++ {
		// Change the first byte
		h = lookupTable[(int(origin[0])+i)%256]

		for _, c := range origin {
			h = lookupTable[h^c]
		}

		hash = append(hash, h)
	}

	return
}
