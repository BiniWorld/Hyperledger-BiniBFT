# Research: `Mir-BFT`

`Author: Siddhant Prateek Mahanayak`

## Concept

Mir-BFT (Mir Byzantine Fault Tolerance) is a consensus algorithm designed to achieve Byzantine fault tolerance in distributed systems.

**Working of `Mir-BFT`** 

- `Node Configuration`: The system consists of a set of nodes that participate in the consensus process. 
- `Leader Election`: In each round, a leader node is selected using a predetermined algorithm. The leader's role is to propose a block of transactions or a system state to the other nodes.
- `Proposal Phase`: The leader broadcasts its proposal to all other nodes. The proposal typically contains the proposed block of transactions.
- `Voting Phase`: Upon receiving the proposal, each node independently validates the proposal and verifies the digital signature of the leader. If the proposal is valid, the node votes for the proposal by sending a signed vote message to all other nodes.
- `Commitment Phase`: After a node collects a sufficient number of votes from other nodes, it sends a commit message to all nodes, indicating that it has received enough support
- `Finalization`: Once a node receives commit messages from a threshold number of nodes, it considers the proposal as finalized. 
- `Next Round`: After finalization, the process repeats for the next round with a new leader elected for the next proposal.

> _Mir-BFT provides string consistency gurantees even in the presence of Byzantine fault, which can include arbitary node failures, malicious nodes or network deplays._

## Setting up Mir-BFT

### Setting up Ubuntu Server


Step-by-step guide to create and set up an Ubuntu EC2 instance on the AWS Free Tier.

**Step 1: Launching an EC2 Instance**

1. Log in to the AWS Management Console.
2. Navigate to the EC2 Dashboard (https://console.aws.amazon.com/ec2/).
3. Click on the "Launch Instance" button.

**Step 2: Select an Amazon Machine Image (AMI)**

1. In the "Choose an Amazon Machine Image (AMI)" section, search for "Ubuntu."
2. Select the desired Ubuntu AMI, usually found under the "Free tier eligible" category.
3. Click on the "Select" button.

**Step 3: Choose an Instance Type**

1. On the "Choose an Instance Type" page, select an instance type that qualifies for the AWS Free Tier (e.g., t2.micro).
2. Click on the "Next: Configure Instance Details" button.

**Step 4: Configure Instance Details**

In this step, you can customize various instance configurations if needed. For the Free Tier setup, the default values should be sufficient. Click on the "Next: Add Storage" button.

**Step 5: Add Storage**

The default storage configuration should be fine for the Free Tier usage. You can adjust the storage size if required. Click on the "Next: Add Tags" button.

**Step 6: Configure Security Group**

1. Create a new security group or select an existing one.
2. Make sure to open at least SSH (port 22) access to connect to your instance remotely.
3. Optionally, open other ports based on your application needs (e.g., HTTP on port 80).
4. Click on the "Review and Launch" button.

**Step 7: Review Instance Launch**

1. Review all the configurations you made in the previous steps.
2. If everything looks good, click on the "Launch" button.

**Step 8: Create a Key Pair**

1. Select "Create a new key pair" from the dropdown.
2. Enter a name for your key pair (e.g., "MyUbuntuInstanceKey").
3. Click on the "Download Key Pair" button to save the .pem file on your local machine.
4. Keep the .pem file secure, as you'll need it to SSH into your instance.

**Step 9: Launch Instances**

1. Click on the "Launch Instances" button.

**Step 10: Connect to Your Instance**

1. Wait for the instance to launch. Once it's running, note down the public IP address assigned to your instance from the EC2 Dashboard.
2. Open a terminal or use an SSH client to connect to your instance using the following command:
   ```
   ssh -i /path/to/your/key.pem ubuntu@your-instance-public-ip
   ```
   Replace "/path/to/your/key.pem" with the actual path to your downloaded .pem key file and "your-instance-public-ip" with your instance's public IP address.

### Setting up mirbft `dev` environment

