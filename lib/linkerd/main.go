package linkerd

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	gonanoid "github.com/matoous/go-nanoid/v2"
	"operators.kloudlite.io/lib/cluster"
)

type LinkerdCli struct {
	cluster *cluster.Client
}

func log(msg string) {
	fmt.Println(msg)
}

func NewLinkerdCli(cluster *cluster.Client) *LinkerdCli {
	return &LinkerdCli{
		cluster: cluster,
	}
}

func (l *LinkerdCli) PreCheck() error {
	log("Checking pre linkerd installation")
	defer log("Checking pre linkerd installation done")

	_, err := l.cluster.Exec("linkerd", "check", "--pre")

	return err
}

func (l *LinkerdCli) Check() error {

	log("Checking linkerd installation")
	defer log("Checking linkerd installation done")

	// _, err := l.cluster.Exec("linkerd", "check")
	// return err

	_, err := l.cluster.Exec("kubectl", "get", "cm/linkerd-config", "-n", "linkerd", "-o", "json")

	return err

}

func (l *LinkerdCli) CheckMC() error {
	log("Checking linkerd mc installation")
	defer log("Checking linkerd mc installation done")

	// _, err := l.cluster.Exec("linkerd", "mc", "check")
	// return err

	_, err := l.cluster.Exec("kubectl", "get", "ns", "linkerd-multicluster", "-o", "json")

	return err

}

func (l *LinkerdCli) Install(trustCerts *string, identityCert *string, identityKey *string) error {
	log("Installing linkerd")
	defer log("Installing linkerd done")

	installOpts := []string{
		"install",
	}

	if trustCerts != nil {
		nanoId, err := gonanoid.New()
		if err != nil {
			return err
		}
		ioutil.WriteFile(fmt.Sprintf("/tmp/%s.crt", nanoId), []byte(*trustCerts), 0644)
		installOpts = append(installOpts, fmt.Sprintf("--identity-trust-anchors-file=/tmp/%s.crt", nanoId))
		defer os.Remove(fmt.Sprintf("/tmp/%s.crt", nanoId))
	}

	if identityCert != nil {
		nanoId, err := gonanoid.New()
		if err != nil {
			return err
		}
		ioutil.WriteFile(fmt.Sprintf("/tmp/%s.crt", nanoId), []byte(*identityCert), 0644)
		installOpts = append(installOpts, fmt.Sprintf("--identity-issuer-certificate-file=/tmp/%s.crt", nanoId))
		defer os.Remove(fmt.Sprintf("/tmp/%s.crt", nanoId))
	}

	if identityKey != nil {
		nanoId, err := gonanoid.New()
		if err != nil {
			return err
		}
		ioutil.WriteFile(fmt.Sprintf("/tmp/%s.key", nanoId), []byte(*identityKey), 0644)
		installOpts = append(installOpts, fmt.Sprintf("--identity-issuer-key-file=/tmp/%s.key", nanoId))
		defer os.Remove(fmt.Sprintf("/tmp/%s.key", nanoId))
	}

	intallYml, err := l.cluster.Exec("linkerd", installOpts...)
	if err != nil {
		return err
	}

	fmt.Println(installOpts)
	return l.cluster.KubectlApply(string(intallYml))
}

func (l *LinkerdCli) Upgrade(trustCerts *string, identityCert *string, identityKey *string) error {
	log("Upgrading linkerd")
	defer log("Upgrading linkerd done")

	installOpts := []string{
		"upgrade",
	}

	if trustCerts != nil {
		nanoId, err := gonanoid.New()
		if err != nil {
			return err
		}
		ioutil.WriteFile(fmt.Sprintf("/tmp/%s.crt", nanoId), []byte(*trustCerts), 0644)
		installOpts = append(installOpts, fmt.Sprintf("--identity-trust-anchors-file=/tmp/%s.crt", nanoId))
		defer os.Remove(fmt.Sprintf("/tmp/%s.crt", nanoId))
	}

	if identityCert != nil {
		nanoId, err := gonanoid.New()
		if err != nil {
			return err
		}
		ioutil.WriteFile(fmt.Sprintf("/tmp/%s.crt", nanoId), []byte(*identityCert), 0644)
		installOpts = append(installOpts, fmt.Sprintf("--identity-issuer-certificate-file=/tmp/%s.crt", nanoId))
		defer os.Remove(fmt.Sprintf("/tmp/%s.crt", nanoId))
	}

	if identityKey != nil {
		nanoId, err := gonanoid.New()
		if err != nil {
			return err
		}
		ioutil.WriteFile(fmt.Sprintf("/tmp/%s.key", nanoId), []byte(*identityKey), 0644)
		installOpts = append(installOpts, fmt.Sprintf("--identity-issuer-key-file=/tmp/%s.key", nanoId))
		defer os.Remove(fmt.Sprintf("/tmp/%s.key", nanoId))
	}

	installYml, err := l.cluster.Exec("linkerd", installOpts...)
	if err != nil {
		fmt.Println("Error:", err)
		return err
	}

	return l.cluster.KubectlApply(string(installYml))
}

func (l *LinkerdCli) InstallMC() error {
	log("Installing linkerd mc")
	defer log("Installing linkerd mc done")

	intallYml, err := l.cluster.Exec("linkerd", "mc", "install")
	if err != nil {
		return err
	}

	return l.cluster.KubectlApply(string(intallYml))
}

func (l *LinkerdCli) Uninstall() error {
	log("Uninstalling linkerd")
	defer log("Uninstalling linkerd done")

	uninstallYml, err := l.cluster.Exec("linkerd", "uninstall")
	if err != nil {
		return err
	}

	return l.cluster.KubectlDelete(string(uninstallYml))
}

func (l *LinkerdCli) UninstallMC() error {
	log("Uninstalling linkerd mc")
	defer log("Uninstalling linkerd mc done")
	uninstallYml, err := l.cluster.Exec("linkerd", "mc", "uninstall")
	if err != nil {
		return err
	}

	return l.cluster.KubectlDelete(string(uninstallYml))
}

// kubectl get deployments.apps -o yaml -n test | linkerd uninject - | kubectl apply -f -

func (l *LinkerdCli) UnInject() error {
	log("Uninjecting linkerd")
	defer log("Uninjecting linkerd done")
	resourceYml, err := l.cluster.Exec("kubectl", "get", "namespaces", "-o", "yaml")

	if err != nil {
		return err
	}

	C := exec.Command("linkerd", "uninject", "-")
	C.Stdin = strings.NewReader(string(resourceYml))

	unInjectYml, err := C.Output()

	if err != nil {
		return err
	}

	return l.cluster.KubectlApply(string(unInjectYml))
}

func (l *LinkerdCli) Inject(namespace string) error {
	log("Injecting linkerd")
	defer log("Injecting linkerd done")

	resourceYml, err := l.cluster.Exec("kubectl", "get", "ns", namespace, "-o", "yaml")

	if err != nil {
		return err
	}

	C := exec.Command("linkerd", "uninject", "-")
	C.Stdin = strings.NewReader(string(resourceYml))

	unInjectYml, err := C.Output()

	if err != nil {
		return err
	}

	return l.cluster.KubectlApply(string(unInjectYml))
}

// step-cli certificate create root.linkerd.cluster.local root.crt root.key \
//    --profile root-ca --no-password --insecure

// step-cli certificate create identity.linkerd.cluster.local issuer.crt issuer.key \
//   --profile intermediate-ca --not-after 8760h --no-password --insecure \
//   --ca root.crt --ca-key root.key

// cat trustAnchor.crt root.crt > bundle.crt

func CreateRootCertificate() ([]byte, []byte, error) {
	log("Creating root certificate")
	defer log("Creating root certificate done")

	savePath := "/tmp/root-cert"

	// if file not exist, create it
	if _, err := os.Stat(savePath); os.IsNotExist(err) {
		os.Mkdir(savePath, 0755)
	}

	defer func() {
		err := os.RemoveAll(savePath)
		if err != nil {
			fmt.Println(err)
		}
	}()

	Cmd := exec.Command("step-cli", "certificate", "create", "root.linkerd.cluster.local", fmt.Sprintf("%s/root.crt", savePath), fmt.Sprintf("%s/root.key", savePath), "--profile", "root-ca", "--no-password", "--insecure")

	err := Cmd.Run()

	if err != nil {
		return nil, nil, err
	}

	cert, err := ioutil.ReadFile(fmt.Sprintf("%s/root.crt", savePath))
	if err != nil {
		return nil, nil, err
	}
	key, err := ioutil.ReadFile(fmt.Sprintf("%s/root.key", savePath))

	if err != nil {
		return nil, nil, err
	}

	return cert, key, nil

}

func GenerateIssuerCert(rootCA, rootKey string) ([]byte, []byte, error) {
	log("Creating issuer certificate")
	defer log("Creating issuer certificate done")

	nanoId, err := gonanoid.New()

	if err != nil {
		return nil, nil, err
	}

	err = ioutil.WriteFile(fmt.Sprintf("/tmp/%s-root.crt", nanoId), []byte(rootCA), 0644)
	if err != nil {
		return nil, nil, err
	}
	defer os.Remove(fmt.Sprintf("/tmp/%s-root.crt", nanoId))

	err = ioutil.WriteFile(fmt.Sprintf("/tmp/%s-root.key", nanoId), []byte(rootKey), 0644)
	if err != nil {
		return nil, nil, err
	}
	defer os.Remove(fmt.Sprintf("/tmp/%s-root.key", nanoId))

	// step-cli certificate create identity.linkerd.cluster.local issuer.crt issuer.key   --profile intermediate-ca --not-after 8760h --no-password --insecure   --ca root.crt --ca-key root.key

	Cmd := exec.Command("step-cli", "certificate", "create", "identity.linkerd.cluster.local", fmt.Sprintf("/tmp/%s-issuer.crt", nanoId), fmt.Sprintf("/tmp/%s-issuer.key", nanoId), "--profile=intermediate-ca", "--not-after=8760h", "--no-password", "--insecure", fmt.Sprintf("--ca=/tmp/%s-root.crt", nanoId), fmt.Sprintf("--ca-key=/tmp/%s-root.key", nanoId))

	err = Cmd.Run()

	if err != nil {
		return nil, nil, err
	}

	cert, err := ioutil.ReadFile(fmt.Sprintf("/tmp/%s-root.crt", nanoId))
	if err != nil {
		return nil, nil, err
	}

	key, err := ioutil.ReadFile(fmt.Sprintf("/tmp/%s-root.key", nanoId))

	if err != nil {
		return nil, nil, err
	}

	return cert, key, nil

}

func (l *LinkerdCli) CheckIsLinked(clusterName string) bool {
	log("Checking if linkerd is linked")
	defer log("Checking if linkerd is linked done")

	// kubectl get links.multicluster.linkerd.io/cluster-sample -n linkerd-multicluster
	_, err := l.cluster.Exec("kubectl", "get", fmt.Sprintf("links.multicluster.linkerd.io/%s", clusterName), "-n", "linkerd-multicluster")

	return err == nil

}

func (l *LinkerdCli) Link(cluster *cluster.Client) error {
	log("Starting linking")
	defer log("done linking")

	// linkerd --kubeconfig ~/.kube/configs/san-francisco-test-kubeconfig.yaml multicluster link --cluster-name=cluster-sample | kubectl --kubeconfig ~/.kube/configs/device-proxy.yml apply -f -
	linkYml, err := cluster.Exec("linkerd", "multicluster", "link", "--cluster-name", cluster.GetName())

	if err != nil {
		return err
	}

	return l.cluster.KubectlApply(string(linkYml))
}
