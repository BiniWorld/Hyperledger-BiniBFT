package main

import (
	"encoding/json"
	"fmt"

	"github.com/hyperledger/fabric-contract-api-go/contractapi"
)

type MangoContract struct {
	contractapi.Contract
}

type Mango struct {
	ID          string  `json:"ID"`
	BatchNumber int     `json:"BatchNumber"`
	Producer    string  `json:"Producer"`
	OwnedBy     string  `json:"OwnedBy"`
	Quantity    int     `json:"Quantity"`
	Price       float32 `json:"Price"`
}

// MangoExists returns true when asset with given ID exists in world state

func (c *MangoContract) MangoExists(ctx contractapi.TransactionContextInterface, mangoID string) (bool, error) {
	data, err := ctx.GetStub().GetState(mangoID)
	if err != nil {
		return false, err
	}
	return data != nil, nil

}

// CreateMango creates a new instance of Mango
func (c *MangoContract) CreateMango(ctx contractapi.TransactionContextInterface, mangoID string, batchNumber int, producer string, quantity int, price float32) error {
	exists, err := c.MangoExists(ctx, mangoID)
	if err != nil {
		return fmt.Errorf("could not read from world state. %s", err)
	} else if exists {
		return fmt.Errorf("the asset %s already exists", mangoID)
	}
	mango := Mango{
		ID:          mangoID,
		BatchNumber: batchNumber,
		Producer:    producer,
		OwnedBy:     producer,
		Quantity:    quantity,
		Price:       price,
	}
	bytes, _ := json.Marshal(mango)
	return ctx.GetStub().PutState(mangoID, bytes)
}

// ReadMango retrieves an instance of Mango from the world state
func (c *MangoContract) ReadMango(ctx contractapi.TransactionContextInterface, mangoID string) (*Mango, error) {
	exists, err := c.MangoExists(ctx, mangoID)
	if err != nil {
		return nil, fmt.Errorf("could not read from world state. %s", err)
	} else if !exists {
		return nil, fmt.Errorf("the asset %s does not exist", mangoID)
	}
	bytes, _ := ctx.GetStub().GetState(mangoID)
	mango := new(Mango)
	err = json.Unmarshal(bytes, mango)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal world state data to type Mango")
	}
	return mango, nil
}

// UpdateMango retrieves an instance of Mango from the world state and updates its value
func (c *MangoContract) UpdateMango(ctx contractapi.TransactionContextInterface, mangoID string, batchNumber int, producer string, owner string, quantity int, price float32) error {
	exists, err := c.MangoExists(ctx, mangoID)
	if err != nil {
		return fmt.Errorf("could not read from world state. %s", err)
	} else if !exists {
		return fmt.Errorf("the asset %s does not exist", mangoID)
	}
	mango := Mango{
		ID:          mangoID,
		BatchNumber: batchNumber,
		Producer:    producer,
		OwnedBy:     owner,
		Quantity:    quantity,
		Price:       price,
	}
	bytes, _ := json.Marshal(mango)
	return ctx.GetStub().PutState(mangoID, bytes)
}

// DeleteMango deletes an instance of Mango from the world state
func (c *MangoContract) DeleteMango(ctx contractapi.TransactionContextInterface, mangoID string) error {
	exists, err := c.MangoExists(ctx, mangoID)
	if err != nil {
		return fmt.Errorf("could not read from world state. %s", err)
	} else if !exists {
		return fmt.Errorf("the asset %s does not exist", mangoID)
	}
	return ctx.GetStub().DelState(mangoID)
}

func (c *MangoContract) SellMango(ctx contractapi.TransactionContextInterface, mangoID string,
	owner string, newOwner string) error {
	exists, err := c.MangoExists(ctx, mangoID)
	if err != nil {
		return fmt.Errorf("could not read from world state. %s", err)
	} else if !exists {
		return fmt.Errorf("the asset %s does not exist", mangoID)
	}
	mango, _ := c.ReadMango(ctx, mangoID)
	if mango.OwnedBy == owner {
		mango.OwnedBy = newOwner
		bytes, _ := json.Marshal(mango)
		return ctx.GetStub().PutState(mangoID, bytes)
	} else {
		return fmt.Errorf("assset is not owned by %v, only original owner can sell the asset", owner)
	}

}

func main() {
	mangoContract := new(MangoContract)
	chaincode, err := contractapi.NewChaincode(mangoContract)
	if err != nil {
		panic("Could not create chaincode." + err.Error())
	}
	err = chaincode.Start()
	if err != nil {
		panic("Failed to start chaincode. " + err.Error())
	}
}
