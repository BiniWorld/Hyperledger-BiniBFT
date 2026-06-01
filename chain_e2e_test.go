package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/sirupsen/logrus"

	smart "github.com/hyperledger/binibft-poc/consensus/pkg/api"
	"github.com/hyperledger/binibft-poc/consensus/pkg/metrics/disabled"
	"github.com/hyperledger/binibft-poc/consensus/pkg/wal"
)

func TestBiniBFTE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "BiniBFT Core Engine E2E Suite")
}

var _ = ginkgo.Describe("BiniBFT Chain and Node Engine", func() {
	var (
		chain1        *Chain
		chain2        *Chain
		tempWalDir1   string
		tempBlocksDir1 string
		tempWalDir2   string
		tempBlocksDir2 string
		opsAddress1   string
		opsAddress2   string
	)

	ginkgo.BeforeEach(func() {
		var err error
		tempWalDir1, err = os.MkdirTemp("", "binibft-wal-1-*")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		tempBlocksDir1, err = os.MkdirTemp("", "binibft-blocks-1-*")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		tempWalDir2, err = os.MkdirTemp("", "binibft-wal-2-*")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		tempBlocksDir2, err = os.MkdirTemp("", "binibft-blocks-2-*")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		met := &disabled.Provider{}
		walMet := wal.NewMetrics(met, "test_wal")
		bftMet := smart.NewMetrics(met, "test_bft")
		
		logger := logrus.New()
		logger.SetLevel(logrus.FatalLevel)

		opsAddress1 = "127.0.0.1:20001"
		opsAddress2 = "127.0.0.1:20002"
		
		mapNodes := make(map[uint64]*NodeInfo)
		mapNodes[1] = &NodeInfo{ID: 1, Address: "127.0.0.1:10001", OpsAddress: opsAddress1}
		mapNodes[2] = &NodeInfo{ID: 2, Address: "127.0.0.1:10002", OpsAddress: opsAddress2}

		opts := NetworkOptions{
			NumNodes:     2,
			BatchSize:    1,
			BatchTimeout: 1 * time.Second,
		}

		ginkgo.By("Bootstrapping a 2-Node BiniBFT Network")
		chain1 = NewChain(1, "127.0.0.1:10001", opsAddress1, mapNodes, logger, walMet, bftMet, opts, tempWalDir1, tempBlocksDir1)
		chain2 = NewChain(2, "127.0.0.1:10002", opsAddress2, mapNodes, logger, walMet, bftMet, opts, tempWalDir2, tempBlocksDir2)

		time.Sleep(1 * time.Second)
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Executing self-cleaning teardown sequence")
		if chain1 != nil && chain1.node != nil {
			chain1.node.Stop()
		}
		if chain2 != nil && chain2.node != nil {
			chain2.node.Stop()
		}
		os.RemoveAll(tempWalDir1)
		os.RemoveAll(tempBlocksDir1)
		os.RemoveAll(tempWalDir2)
		os.RemoveAll(tempBlocksDir2)
	})

	ginkgo.Context("Transaction Ordering and State Integrity", func() {
		ginkgo.It("should successfully accept a raw transaction on Node 1 and expose status via Ops API", func() {
			
			ginkgo.By("Step 1: Constructing and submitting an ASN.1 serialized transaction to Node 1")
			txn := Transaction{
				ClientID: "client-e2e-test",
				TS:       int(time.Now().UnixNano() / 1000000),
				ID:       "txn-uuid-8899",
				Data:     "fund-transfer-payload",
			}

			err := chain1.Order(txn)
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Order engine should not block the transaction")

			ginkgo.By("Step 2: Pinging the Ops API to verify Node 1 is healthy")
			statusURL := fmt.Sprintf("http://%s/status", opsAddress1)
			resp, err := http.Get(statusURL)
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Ops API should be reachable")
			defer resp.Body.Close()

			gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))

			var statusResp map[string]interface{}
			err = json.NewDecoder(resp.Body).Decode(&statusResp)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			
			nodeID := statusResp["nodeID"].(float64)
			gomega.Expect(int(nodeID)).To(gomega.Equal(1))
		})
	})
})