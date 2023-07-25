# Research: `BDLS`

`Author: Siddhant Prateek Mahanayak`


## Setting up BDLS on Linux Server.

### Setting up Linux Server


Step-by-step guide to create and set up an Ubuntu EC2 instance on the AWS Free Tier.

**Prerequisites:**
1. An AWS account (if you don't have one, sign up at https://aws.amazon.com/).
2. Basic knowledge of using AWS Management Console.

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

**Congratulations!** You've successfully created and connected to your Ubuntu EC2 instance on the AWS Free Tier. Now, you can start using the instance for your desired applications, software installations, and projects.

### Setting up BDLS


The provided commands are related to setting up and running the "bdls" (Byzantine Distributed Ledger in the Snow) project from Hyperledger Labs. Below, I'll provide a brief explanation for each command:

```bash
sudo apt-get update
``` 
This command updates the local package index to ensure that the package information is up to date and retrieve the latest package lists from the software repositories.

```bash 
sudo apt-get -y upgrade
```
This command upgrades the installed packages to their latest versions without asking for confirmation (`-y` flag).

```bash 
sudo apt-get install autoconf automake libtool curl make g++ unzip
``` 
This installs the required dependencies (autoconf, automake, libtool, curl, make, g++, and unzip) necessary for building and running the project.

```bash 
cd /tmp
``` 
Changes the current working directory to `/tmp`.

```bash 
wget https://go.dev/dl/go1.17.5.linux-amd64.tar.gz
```
Downloads Go version 1.17.5 for Linux (amd64) from the specified URL.

```bash 
sudo tar -xvf go1.17.5.linux-amd64.tar.gz
```
Extracts the downloaded Go archive.

```bash 
sudo mv go /usr/local
```
Moves the extracted Go folder to the `/usr/local` directory, making it available system-wide.

```bash 
cd
```
Changes the current working directory back to the user's home directory.
```bash 
echo 'export GOROOT=/usr/local/go' >> .profile
``` 
Appends the `GOROOT` environment variable configuration to the `.profile` file. This sets the Go root directory to `/usr/local/go`.

```bash 
echo 'export GOPATH=$HOME/go' >> .profile
```
Appends the `GOPATH` environment variable configuration to the `.profile` file. This sets the Go workspace directory to `$HOME/go`.

```bash 
echo 'export PATH=$GOPATH/bin:$GOROOT/bin:$PATH' >> .profile
``` 
Appends the `PATH` environment variable configuration to the `.profile` file. This adds Go binary paths to the system's executable search paths.

```bash 
source ~/.profile
```
Reloads the updated `.profile` file to apply the changes immediately in the current terminal session.


```bash 
go env
```
Displays Go environment information.

```bash 
git clone https://github.com/hyperledger-labs/bdls.git
```
Clones the "bdls" repository from the specified GitHub URL.

```bash 
cd bdls/
```
Changes the current working directory to the cloned "bdls" repository.

```bash 
git checkout main
```
Switches to the "main" branch of the repository.

```bash 
cd cmd/emucon/
```
Changes the current working directory to `cmd/emucon/` within the repository.

```bash 
go build .
``` 
Compiles the "emucon" command-line tool.

```bash 
./emucon help genkeys
```
Displays help information for the "genkeys" command of "emucon".

```bash 
./emucon genkeys --count 4
```
Generates four cryptographic key pairs using "emucon".

> Instructions to run four participants: After generating the cryptographic key pairs, it instructs the user to open four terminals and run four instances of "emucon" with different participant IDs and listening ports.


```bash 
go test -v -cpuprofile=cpu.out -memprofile=mem.out -timeout 2h
```
Runs tests for the "bdls" project with verbose output (`-v`), profiling the CPU usage (`-cpuprofile`), profiling the memory usage (`-memprofile`), and setting a test timeout of 2 hours.
