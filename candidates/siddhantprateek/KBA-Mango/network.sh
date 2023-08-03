export MICROFAB_CONFIG='{
"port": 8080,
"endorsing_organizations":[
{
"name": "ProducersOrg"
},
{
"name": "SellersOrg"
}
],
"channels":[
{
"name": "mango-channel",
"endorsing_organizations":[
"ProducersOrg",
"SellersOrg"
]
}
]
}'