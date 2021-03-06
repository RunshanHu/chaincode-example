/* 
    chaincode for sharing history storage

    author: Runshan Hu
*/

package main

import (
        "errors"
        "fmt"
        "math"
        "log"
        "encoding/json"
        "net/http"
        "net/url"
        "io/ioutil"
        "strconv"
        "bytes"
        "github.com/hyperledger/fabric/core/chaincode/shim"
)

const SMALL_BUDGET = 0.05
const UTILITY_BOUND = 1500

type SimpleChaincode struct {
}

// value format for ledger
type ledgerMes struct {
  RemainBudget     float64   `json:"budget"`
  FunType          []string  `json:"funType"`
  Result           []float64 `json:"results"`
}

// message format for query
type queryMes struct {
  RequestBudget    float64   `json:"budget"`
  FunType          string    `json:"funType"`
}

var logger = shim.NewLogger("chaincode_sharing_history")

func main() {
        logger.Info("--->main called")
        err := shim.Start(new(SimpleChaincode))
        if err != nil {
                fmt.Printf("Error starting sharing historty storage chaincode: %s", err)
        }
}

//Init resets all the things
func (t *SimpleChaincode) Init(stub shim.ChaincodeStubInterface, function string, args []string) ([]byte, error){
        if len(args) != 2 {
                return nil, errors.New("Incorrect number of arguments. Expecting 2")
        }

        err := stub.PutState(args[0], []byte(args[1]))
        if err != nil {
                return nil, err
        }
        return nil, nil
}

//Invoke entry point to invoke a chaincode function
func (t *SimpleChaincode) Invoke(stub shim.ChaincodeStubInterface, function string, args []string) ([]byte, error) {
        fmt.Println("invoke is running" + function)

        //Handle different functions
        if function == "init" {
                return t.Init(stub, "init", args)
        } else if function == "queryMatchTest" {
                logger.Info("--->queryMatchTest invoked")
                return t.queryMatchTest(stub, args)
        }

        fmt.Println("invoke did not find func:" + function)

        return nil, errors.New("Received unknown function invocation")

}


func (t *SimpleChaincode) queryMatchTest(stub shim.ChaincodeStubInterface, args []string) ([]byte, error) {
       
       var str string

       logger.Info("--->queryMatchTest called!")

       var dataId string
       var value string
       var err error

       // args should have two parameter: datasetId and user's query
       if len(args) != 2 {
               return nil, errors.New("Incorrect number of arguments. Expecting 2. DatasetId and your query ")
       }
       
       dataId = args[0];
       value = args[1];
       
       //parser user's query 
       mes_from_query := queryMes{}
       json.Unmarshal([]byte(value), &mes_from_query);

       logger.Info("--->parser user's query: ", mes_from_query)

       // get the old query from ledger
       valAsbytes, err := stub.GetState(dataId);
       if err != nil {
                jsonResp := "{\"Error\": \"Failed to get the state for " + dataId + "\"}"
                return nil, errors.New(jsonResp) 
       }

       // parser the old query (from ledger)
       mes_from_ledger := ledgerMes{}
       json.Unmarshal(valAsbytes, &mes_from_ledger)
       
       logger.Info("--->got the state from the ledger: ", mes_from_ledger)

       flag := false;   //whether query exists before
       var old_result, final_result, perturbed_result float64
       var i int
       var e string
       
       for i, e = range mes_from_ledger.FunType {
                if e == mes_from_query.FunType {
                        if mes_from_ledger.Result[i] > 0 {
                                flag = true 
                                break 
                        }
                } 
       }
       // if old result exist
       if flag {
                // old result (from ledger)
                logger.Info("--->old result exists on the ledger")

                old_result = mes_from_ledger.Result[i]
                // get perturbed result from anonymisation service
                perturbed_result = getResultAnonyService(mes_from_query.FunType, SMALL_BUDGET, 1)
                // utility test
                
                logger.Info("--->got the perturbed result from anonymisation service(using small budget): ", perturbed_result)

                if math.Abs(old_result - perturbed_result) < UTILITY_BOUND {

                        logger.Info("--->perturbed result pass the utility test! Use this result for user's query!")
                        
                        final_result =  perturbed_result
                        updateLedger(stub, dataId, mes_from_query.FunType, final_result, SMALL_BUDGET)

                        str = fmt.Sprintf("--->old result exists and perturbed result pass the utility test! result: %f", final_result)
                        
                } else {
                        logger.Info("--->perturbed result not pass the utility test! check if satify budget verification")
                        if mes_from_ledger.RemainBudget >= mes_from_query.RequestBudget  {
                              logger.Info("--->Pass the budget verification! Getting the new result from anonymisation service(using requested budget): ")
                              final_result = getResultAnonyService(mes_from_query.FunType, mes_from_query.RequestBudget, 0)
                              // updateLedger()
                              updateLedger(stub, dataId, mes_from_query.FunType, final_result, mes_from_query.RequestBudget)

                              str = fmt.Sprintf("--->old result exists but perturbed result not pass the utility test, budget satify! result: %f", final_result)
                              
                        } else {
                              logger.Info("--->Do not pass the budget verification! Not return any result for the user! (-1000)")
                              final_result = -1000 
                              // updateLedger()
                              logger.Info("--->Still updating ledger using small budget and perturbed result..")
                              updateLedger(stub, dataId, mes_from_query.FunType, perturbed_result, SMALL_BUDGET)

                              str = fmt.Sprintf("--->old result exists, perturbed result not  pass the utility test, budget not enough, no result!")
                        }
                }
       } else { // old result not exist
                logger.Info("--->Old result not exist! Check if satify budget verification")
                if mes_from_ledger.RemainBudget >= mes_from_query.RequestBudget  {
                        logger.Info("--->Pass the budget verification! Getting the new result from anonymisation service(using requested, budget): ")
                        final_result = getResultAnonyService(mes_from_query.FunType, mes_from_query.RequestBudget, 0)
                        //updateLedger()
                        updateLedger(stub, dataId, mes_from_query.FunType, final_result, mes_from_query.RequestBudget)

                        str = fmt.Sprintf("--->old result not exist, budget satify, result: %f", final_result)

                } else {
                        logger.Info("--->Do not pass the budget verification! Not return any result for the user! (-1000)")
                        final_result = -1000 
                        logger.Info("--->No update of the ledger")

                        str = fmt.Sprintf("--->old result not exist, budget not enough, no result!")

                }
       }
       return []byte(str), nil 
}

func updateLedger(stub shim.ChaincodeStubInterface, dataId string, funType string, newResult float64, subBudget float64) (error) {

        logger.Info("--->updateLedger called")

        valAsbytes, err := stub.GetState(dataId)
        if err != nil {
          jsonResp := "{\"Error\": \"Failed to get the state for " + dataId + "\"}"
                 return errors.New(jsonResp) 
        }
        
        newValue := ledgerMes{} 
        json.Unmarshal(valAsbytes, &newValue)
        newValue.RemainBudget = newValue.RemainBudget - subBudget

        var index int
        for i,e := range newValue.FunType {
                if e == funType {
                        index = i
                        break
                }
        }
        newValue.Result[index] = newResult
        newValue_json,err := json.Marshal(newValue)
        
        // write back to the ledger
        err = stub.PutState(dataId, []byte(newValue_json))
        if err != nil {
               return err 
        }

        logger.Info("--->updating ledger, newBudget: ", newValue.RemainBudget, ", FunctionType: ", funType, ", newVale: ", newResult)

        return nil
}

type serviceResult struct {
         Result float64 `json:"result"`
}

func getResultAnonyService( funtype string, budget float64, flag int  ) float64  {

        logger.Info("--->getResultAnonyService called")
        var resp *http.Response
        var err error
        normalResp := true;

        data := url.Values{};
        data.Set("budget", strconv.FormatFloat(budget, 'f', -1, 64));
        data.Add("flag", strconv.Itoa(flag));
        inputbody := bytes.NewBufferString(data.Encode());

        switch funtype {
               case "sum": 
                         resp, err = http.Post("http://10.7.6.25:3000/dataset/sum", "application/x-www-form-urlencoded", inputbody)
               case "avg": 
                         resp, err = http.Post("http://10.7.6.25:3000/dataset/avg", "application/x-www-form-urlencoded", inputbody) 
               case "max": 
                         resp, err = http.Post("http://10.7.6.25:3000/dataset/max", "application/x-www-form-urlencoded", inputbody)
               case "min": 
                         resp, err = http.Post("http://10.7.6.25:3000/dataset/min", "application/x-www-form-urlencoded", inputbody)
               default:{
                         log.Println("unrecognized function type")
                         normalResp = false; 
                       }
        } 
        if err != nil {
                log.Println(err);
                normalResp = false;
        }

        if normalResp {
               
               body,err := ioutil.ReadAll(resp.Body);
               if err != nil {
                         log.Println(err);
               }

               defer resp.Body.Close();

               result := serviceResult{}

               if err := json.Unmarshal(body, &result); err != nil {
                         log.Println(err); 
               }

               logger.Info("--->got the result from anonymisation service: ", funtype, " : ", result.Result)

               return result.Result;
        } else {
               
               return -1000;
        }
}
//Query is entry point for queries
func (t *SimpleChaincode) Query(stub shim.ChaincodeStubInterface, function string, args []string) ([]byte, error) {
        var key, jsonResp string
        var err error

        if len(args) != 1 {
                return nil, errors.New("Incorrect number of arguments. Expecting name of the dataId to query")
        }

        key = args[0]
        valAsbytes, err := stub.GetState(key)

        if err != nil {
                jsonResp = "{\"Error\": \"Failed to get the state for " + key + "\"}"
                return nil, errors.New(jsonResp)
        }

        return valAsbytes, nil
}

/* Test only: write - invoke function to write key/value pair
func (t *SimpleChaincode) write(stub shim.ChaincodeStubInterface, args []string) ([]byte, error) {
        var datasetId, value string
        var err error
        fmt.Println("running write()")

        if len(args) != 2 {
                return nil, errors.New("Incorrect number of arguments. Expecting 2. DatasetID and value to set")
        }

        datasetId = args[0]
        value = args[1]
        
        //write the variable into the chaincode state
        err = stub.PutState(datasetId, []byte(value))
        if err != nil {
                return nil, err
        }

        return nil, nil
}
*/
