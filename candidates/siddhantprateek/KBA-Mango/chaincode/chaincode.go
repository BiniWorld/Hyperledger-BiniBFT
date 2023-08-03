package chaincode

import (
	"KBA-Mango/models"
	"encoding/json"
	"fmt"

	"github.com/hyperledger/fabric-contract-api-go/contractapi"
)

type MangoContract struct {
	contractapi.Contract
}

// MangoExists
func (c *MangoContract) MangoExists(ctx contractapi.TransactionContextInterface, mangoId string) (bool, error) {
	data, err := ctx.GetStub().GetState(mangoId)
	if err != nil {
		return false, err
	}
	return data != nil, nil
}

// Create Mango
func (c *MangoContract) CreateMango(ctx contractapi.TransactionContextInterface, mangoID string, batchNumber int,
	producer string, quantity int, price float32) error {

	exists, err := c.MangoExists(ctx, mangoID)
	if err != nil {
		return fmt.Errorf("could not read from world state. %s", err)
	} else if exists {
		return fmt.Errorf("the asset %s already exists", mangoID)
	}

	mango := models.Mango{
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

func (c *MangoContract) ReadMango(ctx contractapi.TransactionContextInterface, mangoID string) (*models.Mango, error) {
	exists, err := c.MangoExists(ctx, mangoID)
	if err != nil {
		return nil, fmt.Errorf("could not read from world state. %s", err)
	} else if !exists {
		return nil, fmt.Errorf("the asset %s does not exist", mangoID)
	}
	bytes, _ := ctx.GetStub().GetState(mangoID)
	mango := new(models.Mango)
	err = json.Unmarshal(bytes, mango)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal world state data to type Mango")
	}
	return mango, nil
}

func (c *MangoContract) UpdateMango(ctx contractapi.TransactionContextInterface, mangoID string, batchNumber int, producer string, owner string, quantity int, price float32) error {
	exists, err := c.MangoExists(ctx, mangoID)
	if err != nil {
		return fmt.Errorf("could not read from world state. %s", err)
	} else if !exists {
		return fmt.Errorf("the asset %s does not exist", mangoID)
	}
	mango := models.Mango{
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

func (c *MangoContract) DeleteMango(ctx contractapi.TransactionContextInterface, mangoID string) error {
	exists, err := c.MangoExists(ctx, mangoID)
	if err != nil {
		return fmt.Errorf("could not read from world state. %s", err)
	} else if !exists {
		return fmt.Errorf("the asset %s does not exist", mangoID)
	}
	return ctx.GetStub().DelState(mangoID)
}
