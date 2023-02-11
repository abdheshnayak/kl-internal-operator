package nameserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type NameServer struct {
	endpoint string
	user     string
	password string
}

func NewClient(endpoint, user, password string) *NameServer {
	return &NameServer{
		endpoint: endpoint,
		user:     user,
		password: password,
	}
}

func (n *NameServer) UpsertDomain(domainName string, aRecords []string) error {
	client := http.Client{Timeout: 5 * time.Second}
	data, err := json.Marshal(
		map[string]any{
			"domain":   domainName,
			"aRecords": aRecords,
		},
	)

	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/upsert-domain", n.endpoint), bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	req.SetBasicAuth(n.user, n.password)
	_, err = client.Do(req)
	return err
}

func (n *NameServer) UpsertNodeIps(region string, accountName string, clusterName string, ips []string) error {
	client := http.Client{Timeout: 5 * time.Second}
	data, err := json.Marshal(
		map[string]any{
			"region":  region,
			"account": accountName,
			"cluster": clusterName,
			"ips":     ips,
		},
	)

	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/upsert-node-ips", n.endpoint), bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	req.SetBasicAuth(n.user, n.password)
	_, err = client.Do(req)
	return err
}

func (n *NameServer) DeleteDomain(domainName string) error {
	req, err := http.NewRequest("DELETE", fmt.Sprintf("%s/delete-domain/%s", n.endpoint, domainName), nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(n.user, n.password)

	Client := http.Client{}
	_, err = Client.Do(req)

	return err
}

func (n *NameServer) GetRecord(domainName string) (*http.Response, error) {

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/get-records/%s", n.endpoint, domainName), nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(n.user, n.password)

	Client := http.Client{}
	return Client.Do(req)
}

func (n *NameServer) GetRegionDomain(accountName, regionId string) (*http.Response, error) {

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/get-region-domain/%s/%s", n.endpoint, accountName, regionId), nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(n.user, n.password)

	Client := http.Client{}
	return Client.Do(req)
}
