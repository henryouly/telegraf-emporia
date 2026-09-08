package clients

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	cognito "github.com/aws/aws-sdk-go/service/cognitoidentityprovider"
)

type CognitoClient interface {
	SignIn(email string, password string) (*cognito.InitiateAuthOutput, error)
}

type awsCognitoClient struct {
	cognitoClient *cognito.CognitoIdentityProvider
	appClientId   string
}

func NewCognitoClient(cognitoRegion string, cognitoAppClientId string) CognitoClient {
	conf := &aws.Config{Region: aws.String(cognitoRegion)}

	sess, err := session.NewSession(conf)
	client := cognito.New(sess)

	if err != nil {
		panic(err)
	}

	return &awsCognitoClient{
		cognitoClient: client,
		appClientId:   cognitoAppClientId,
	}
}

func (ctx *awsCognitoClient) SignIn(email string, password string) (*cognito.InitiateAuthOutput, error) {
	initiateAuthInput := &cognito.InitiateAuthInput{
		AuthFlow: aws.String("USER_PASSWORD_AUTH"),
		AuthParameters: aws.StringMap(map[string]string{
			"USERNAME": email,
			"PASSWORD": password,
		}),
		ClientId: aws.String(ctx.appClientId),
	}
	result, err := ctx.cognitoClient.InitiateAuth(initiateAuthInput)

	return result, err
}
