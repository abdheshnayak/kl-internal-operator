package infra

type doSpec struct {
	Provider doProvider `yaml:"provider" json:"provider"`
	Node     doNode     `yaml:"node" json:"node"`
}

type awsSpec struct {
	Provider awsProvider `yaml:"provider" json:"provider"`
	Node     awsNode     `yaml:"node" json:"node"`
}

type doProvider struct {
	ApiToken  string `yaml:"apiToken" json:"apiToken"`
	AccountId string `yaml:"accountId" json:"accountId"`
}

type awsProvider struct {
	AccessKey    string `yaml:"accessKey" json:"accessKey"`
	AccessSecret string `yaml:"accessSecret" json:"accessSecret"`
	AccountId    string `yaml:"accountId" json:"accountId"`
}

type doNode struct {
	Region  string `yaml:"region" json:"region"`
	Size    string `yaml:"size" json:"size"`
	NodeId  string `yaml:"nodeId,omitempty" json:"nodeId,omitempty"`
	ImageId string `yaml:"imageId" json:"imageId"`
}

type awsNode struct {
	NodeId       string `yaml:"nodeId,omitempty" json:"nodeId,omitempty"`
	Region       string `yaml:"region" json:"region"`
	InstanceType string `yaml:"instanceType" json:"instanceType"`
	VPC          string `yaml:"vpc,omitempty" json:"vpc,omitempty"`
	AMI          string `yaml:"ami" json:"ami"`
}

type doConfig struct {
	Version  string `yaml:"version" json:"version"`
	Action   string `yaml:"action" json:"action"`
	Provider string `yaml:"provider" json:"provider"`
	Spec     doSpec `yaml:"spec" json:"spec"`
}

type awsConfig struct {
	Version  string  `yaml:"version" json:"version"`
	Action   string  `yaml:"action" json:"action"`
	Provider string  `yaml:"provider" json:"provider"`
	Spec     awsSpec `yaml:"spec" json:"spec"`
}

type doKLConfValues struct {
	ServerUrl   string `yaml:"serverUrl" json:"serverUrl"`
	SshKeyPath  string `yaml:"sshKeyPath" json:"sshKeyPath"`
	StorePath   string `yaml:"storePath" json:"storePath"`
	TfTemplates string `yaml:"tfTemplatesPath" json:"tfTemplatesPath"`
	JoinToken   string `yaml:"joinToken" json:"joinToken"`
}

type doKLConf struct {
	Version string         `yaml:"version" json:"version"`
	Values  doKLConfValues `yaml:"spec" json:"spec"`
}
