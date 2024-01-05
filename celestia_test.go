package celestia_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ory/dockertest/v3"
	"github.com/stretchr/testify/suite"

	rpc "github.com/celestiaorg/celestia-node/api/rpc/client"
	"github.com/celestiaorg/celestia-node/share"

	"github.com/rollkit/celestia-da"
	"github.com/rollkit/go-da/test"
)

type TestSuite struct {
	suite.Suite

	pool     *dockertest.Pool
	resource *dockertest.Resource

	token string
}

func (t *TestSuite) SetupSuite() {
	pool, err := dockertest.NewPool("")
	if err != nil {
		t.Failf("Could not construct docker pool", "error: %v\n", err)
	}
	t.pool = pool

	// uses pool to try to connect to Docker
	err = pool.Client.Ping()
	if err != nil {
		t.Failf("Could not connect to Docker", "error: %v\n", err)
	}

	// pulls an image, creates a container based on it and runs it
	resource, err := pool.Run("ghcr.io/rollkit/local-celestia-devnet", "76be922", []string{})
	if err != nil {
		t.Failf("Could not start resource", "error: %v\n", err)
	}
	t.resource = resource

	// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
	pool.MaxWait = 60 * time.Second
	if err := pool.Retry(func() error {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%s/balance", resource.GetPort("26659/tcp")))
		if err != nil {
			return err
		}
		bz, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return err
		}
		if strings.Contains(string(bz), "error") {
			return errors.New(string(bz))
		}
		return nil
	}); err != nil {
		log.Fatalf("Could not start local-celestia-devnet: %s", err)
	}

	opts := dockertest.ExecOptions{}
	buf := new(bytes.Buffer)
	opts.StdOut = buf
	opts.StdErr = buf
	_, err = resource.Exec([]string{"/bin/celestia-da", "bridge", "auth", "admin", "--node.store", "/home/celestia/bridge"}, opts)
	if err != nil {
		t.Failf("Could not execute command", "error: %v\n", err)
	}

	t.token = strings.TrimSpace(buf.String())
}

func (t *TestSuite) TearDownSuite() {
	if err := t.pool.Purge(t.resource); err != nil {
		t.Failf("failed to purge docker resource", "error: %v\n", err)
	}
}

func TestIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func (t *TestSuite) TestCelestiaDA() {
	client, err := rpc.NewClient(context.Background(), t.getRPCAddress(), t.token)
	t.Require().NoError(err)
	ns, err := share.NewBlobNamespaceV0([]byte("test"))
	t.Require().NoError(err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	da := celestia.NewCelestiaDA(client, ns, ctx)
	test.RunDATestSuite(t.T(), da)
}

func (t *TestSuite) getRPCAddress() string {
	return fmt.Sprintf("http://localhost:%s", t.resource.GetPort("26658/tcp"))
}
