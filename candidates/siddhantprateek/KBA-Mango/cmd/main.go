package main

import (
	"KBA-Mango/chaincode"

	"github.com/hyperledger/fabric-contract-api-go/contractapi"
)

func main() {

	mangoContract := new(chaincode.MangoContract)
	chaincode, err := contractapi.NewChaincode(mangoContract)
	if err != nil {
		panic("could not create chaincode." + err.Error())
	}
	err = chaincode.Start()
	if err != nil {
		panic("Failed to start chaincode. " + err.Error())
	}
}
