package worker

import("net/netip";"testing")
func TestPrivateGatewayAddressesRejected(t *testing.T){
 for _,ip:=range []string{"127.0.0.1","10.1.2.3","192.168.1.6","100.64.0.1","169.254.169.254","::1","fc00::1","::ffff:127.0.0.1","2001:db8::1"}{if publicIP(netip.MustParseAddr(ip)){t.Fatalf("private/reserved address allowed: %s",ip)}}
 for _,ip:=range []string{"1.1.1.1","8.8.8.8","2606:4700:4700::1111"}{if !publicIP(netip.MustParseAddr(ip)){t.Fatalf("public address rejected: %s",ip)}}
}
