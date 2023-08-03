package models

type Mango struct {
	ID          string  `json:"ID"`
	BatchNumber int     `json:"BatchNumber"`
	Producer    string  `json:"Producer"`
	OwnedBy     string  `json:"OwnedBy"`
	Quantity    int     `json:"Quantity"`
	Price       float32 `json:"Price"`
}
