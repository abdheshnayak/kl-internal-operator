package nameserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type NameServer struct {
	endpoint string
}

func NewClient(endpoint string) *NameServer {
	return &NameServer{endpoint}
}

func (n *NameServer) UpsertDomain(domainName string, aRecords []string) error {
	data, err := json.Marshal(map[string]any{
		"domain":   domainName,
		"aRecords": aRecords,
	})

	if err != nil {
		return err
	}

	_, err = http.Post(fmt.Sprintf("%s/upsert-domain", n.endpoint), "application/json", bytes.NewBuffer(data))

	if err != nil {
		return err
	}
	return nil
}

func (n *NameServer) UpsertNodeIps(region string, ips []string) error {
	data, err := json.Marshal(map[string]any{
		"region": region,
		"ips":    ips,
	})

	if err != nil {
		return err
	}

	_, err = http.Post(fmt.Sprintf("%s/upsert-node-ips", n.endpoint), "application/json", bytes.NewBuffer(data))

	if err != nil {
		return err
	}
	return nil
}

func (n *NameServer) DeleteDomain(domainName string) error {
	req, err := http.NewRequest("DELETE", fmt.Sprintf("%s/delete-domain/%s", n.endpoint, domainName), nil)
	if err != nil {
		return err
	}

	Client := http.Client{}

	_, err = Client.Do(req)

	return err
}
