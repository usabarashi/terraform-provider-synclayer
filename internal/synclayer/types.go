package synclayer

// About mirrors GET /api/v1/gateway/about.
type About struct {
	ModelName string `json:"modelName"`
	SerialNo  string `json:"serialNo"`
	Hardware  struct {
		Version string `json:"version"`
	} `json:"hardware"`
	Software struct {
		Version   string `json:"version"`
		BuildTime string `json:"buildTime"`
	} `json:"software"`
	OperationTime string `json:"operationTime"`
	BaseMAC       string `json:"baseMac"`
}

// PortRange is an inclusive port interval used by port forwarding rules.
type PortRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// PortForwardingRule mirrors one entry of GET /api/v1/service/portForwarding.
type PortForwardingRule struct {
	ID           int       `json:"id,omitempty"`
	Active       bool      `json:"active"`
	Protocol     string    `json:"protocol"`
	IPAddress    string    `json:"ipAddress"`
	LocalPort    PortRange `json:"localPort"`
	ExternalPort PortRange `json:"externalPort"`
	ServiceType  string    `json:"serviceType"`
}

type portForwardingList struct {
	Rules    []PortForwardingRule `json:"rules"`
	Active   bool                 `json:"active"`
	MaxRules int                  `json:"maxRules"`
}

// StaticRoute mirrors one entry of GET /api/v1/service/staticRoute.
type StaticRoute struct {
	ID            int    `json:"id,omitempty"`
	Active        bool   `json:"active"`
	Status        string `json:"status,omitempty"`
	DestinationIP string `json:"destinationIP"`
	Subnet        string `json:"subnet"`
	Gateway       string `json:"gateway"`
	Interface     int    `json:"interface"`
	IfName        string `json:"ifName,omitempty"`
}

// WanInterface describes an interface that a static route can egress through.
type WanInterface struct {
	IfName string `json:"ifName"`
	Name   string `json:"name"`
}

type staticRouteList struct {
	Active                bool           `json:"active"`
	List                  []StaticRoute  `json:"list"`
	AvailableWanInterface []WanInterface `json:"availableWanInterface"`
	MaxRules              int            `json:"maxRules"`
}

// ReservedIP mirrors one entry of GET /api/v1/service/reservedIP.
type ReservedIP struct {
	ID         int    `json:"id,omitempty"`
	MacAddress string `json:"macAddress"`
	IPAddress  string `json:"ipAddress"`
	DeviceName string `json:"deviceName"`
	// DeviceID is always sent as null by the web UI and is not surfaced to
	// Terraform.
	DeviceID *int `json:"deviceId"`
}

type reservedIPList struct {
	Rules    []ReservedIP `json:"rules"`
	Active   bool         `json:"active"`
	MaxRules int          `json:"maxRules"`
	Subnet   string       `json:"subnet"`
}

// DDNSResult is the runtime state reported by the DDNS subsystem.
type DDNSResult struct {
	ConnectionStatus string `json:"connectionStatus"`
	ReturnCode       int    `json:"returnCode"`
	IPAddress        string `json:"ipAddress"`
}

// DDNSProvider describes one selectable DDNS provider.
type DDNSProvider struct {
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	DomainName []string `json:"domainName"`
}

// DDNSConfiguration is the mutable DDNS configuration.
type DDNSConfiguration struct {
	CurrentProvider int    `json:"currentProvider"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	Token           string `json:"token"`
	Hostname        string `json:"hostname"`
	URL             string `json:"url"`
}

// DDNS mirrors GET /api/v1/service/ddns.
type DDNS struct {
	Active             bool              `json:"active"`
	Result             DDNSResult        `json:"result"`
	SupportingProvider []DDNSProvider    `json:"supportingProvider"`
	Configuration      DDNSConfiguration `json:"configuration"`
}

// DMZ mirrors GET /api/v1/service/dmz.
type DMZ struct {
	Active      bool   `json:"active"`
	Destination string `json:"destination"`
	Subnet      string `json:"subnet"`
}
